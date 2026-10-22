// Package mongo is the MongoDB driver. A "table" is a collection; statements are Mongo-shell
// JavaScript (evaluated with goja) or raw extended-JSON commands.
package mongo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mdb "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func init() { db.Register(driver{}) }

type driver struct{}

func (driver) Name() string { return config.Mongo }

type dialer db.DialFunc

func (d dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

type session struct {
	client *mdb.Client
	info   string

	mu    sync.Mutex
	cur   string
	cache map[string]*colInfo // "db.coll" -> sampled structure
}

func tlsConfig(c config.Connection) (*tls.Config, error) {
	switch c.SSLMode {
	case "", "disable", "prefer": // MongoDB has no opportunistic TLS: prefer means plain
		return nil, nil
	case "require", "verify-ca", "verify-full", "verify-identity":
	default:
		return nil, fmt.Errorf("unsupported ssl_mode %q for mongodb", c.SSLMode)
	}
	cfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.SSLCA != "" {
		pem, err := os.ReadFile(c.SSLCA)
		if err != nil {
			return nil, fmt.Errorf("ssl_ca: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ssl_ca %s: no certificates found", c.SSLCA)
		}
	}
	if c.SSLCert != "" || c.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(c.SSLCert, c.SSLKey)
		if err != nil {
			return nil, fmt.Errorf("ssl_cert/ssl_key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	switch c.SSLMode {
	case "require":
		cfg.InsecureSkipVerify = true
	case "verify-ca": // verify the chain but not the host name
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("server sent no certificate")
			}
			opts := x509.VerifyOptions{Roots: cfg.RootCAs, Intermediates: x509.NewCertPool()}
			for _, ic := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(ic)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		}
	}
	return cfg, nil
}

func (driver) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	tc, err := tlsConfig(c)
	if err != nil {
		return nil, err
	}
	opts := options.Client().
		SetHosts([]string{net.JoinHostPort(c.Host, strconv.Itoa(c.EffectivePort()))}).
		SetDirect(true).SetMaxPoolSize(8).SetAppName("tuidb").
		SetServerSelectionTimeout(10 * time.Second)
	if tc != nil {
		opts.SetTLSConfig(tc)
	}
	if dial != nil {
		opts.SetDialer(dialer(dial))
	}
	if c.User != "" {
		src := c.Options["authSource"]
		if src == "" {
			src = "admin"
		}
		opts.SetAuth(options.Credential{AuthSource: src, Username: c.User, Password: c.Password, PasswordSet: c.Password != ""})
	}
	cl, err := mdb.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongodb connect: %w", err)
	}
	s := &session{client: cl, cur: c.Database, cache: map[string]*colInfo{}}
	if s.cur == "" {
		s.cur = "test"
	}
	fail := func(err error) (db.Session, error) {
		_ = s.Close()
		return nil, err
	}
	if err := cl.Ping(ctx, nil); err != nil {
		return fail(fmt.Errorf("mongodb ping: %w", err))
	}
	bi, err := s.runCmd(ctx, "admin", bson.D{{Key: "buildInfo", Value: 1}})
	if err != nil {
		return fail(fmt.Errorf("mongodb buildInfo: %w", err))
	}
	if v, _ := lookup(bi, "version"); v != nil {
		s.info = fmt.Sprintf("MongoDB %v", v)
	}
	for _, b := range c.Bootstrap {
		if _, err := s.Exec(ctx, b, 0); err != nil {
			return fail(fmt.Errorf("bootstrap %q: %w", b, err))
		}
	}
	return s, nil
}

func (s *session) Caps() db.Capabilities {
	return db.Capabilities{Indexes: true, Views: true, Document: true, Editor: "mongo", Dialect: "mongo", DatabaseNoun: "database"}
}

func (s *session) ServerInfo() string                        { return s.info }
func (s *session) Ping(ctx context.Context) error            { return s.client.Ping(ctx, nil) }
func (s *session) CurrentSchema() string                     { return "" }
func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error) {
	return nil, nil
}
func (s *session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) {
	return nil, nil
}
func (s *session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	return nil, errors.New("MongoDB collections have no fixed structure to edit")
}

func (s *session) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *session) CurrentDatabase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *session) UseDatabase(_ context.Context, name string) error {
	if name == "" {
		return errors.New("database name is empty")
	}
	s.mu.Lock()
	s.cur = name
	s.mu.Unlock()
	return nil
}

func (s *session) databases(ctx context.Context) ([]string, error) {
	names, err := s.client.ListDatabaseNames(ctx, bson.D{}, options.ListDatabases().SetAuthorizedDatabases(true))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func (s *session) Databases(ctx context.Context) ([]string, error) {
	n, err := s.databases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	return n, nil
}

// runCmd runs a command and turns writeErrors into an error.
func (s *session) runCmd(ctx context.Context, dbn string, cmd bson.D) (bson.D, error) {
	var out bson.D
	if err := s.client.Database(dbn).RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, err
	}
	if we, ok := lookup(out, "writeErrors"); ok {
		if arr, _ := we.(bson.A); len(arr) > 0 {
			if d, ok := arr[0].(bson.D); ok {
				if m, _ := lookup(d, "errmsg"); m != nil {
					return out, fmt.Errorf("%v", m)
				}
			}
		}
	}
	return out, nil
}

func (s *session) Objects(ctx context.Context, _ string) ([]db.Object, error) {
	dbn := s.CurrentDatabase()
	d := s.client.Database(dbn)
	specs, err := d.ListCollectionSpecifications(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	var out []db.Object
	for _, sp := range specs {
		if strings.HasPrefix(sp.Name, "system.") {
			continue
		}
		o := db.Object{ObjRef: db.ObjRef{Name: sp.Name, Kind: db.KindTable}}
		if sp.Type == "view" {
			o.Kind = db.KindView
		}
		out = append(out, o)
	}
	// Document counts are one round trip each: run them concurrently (bounded), not one after another.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range out {
		if out[i].Kind == db.KindView {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(o *db.Object) {
			defer func() { <-sem; wg.Done() }()
			if n, err := d.Collection(o.Name).EstimatedDocumentCount(ctx); err == nil {
				o.Detail = strconv.FormatInt(n, 10)
			}
		}(&out[i])
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, nil
}

// sample reads the first 100 documents and caches the inferred field types.
func (s *session) sample(ctx context.Context, coll string) (*colInfo, error) {
	dbn := s.CurrentDatabase()
	cur, err := s.client.Database(dbn).Collection(coll).Find(ctx, bson.D{}, options.Find().SetLimit(100))
	if err != nil {
		return nil, fmt.Errorf("sample %s: %w", coll, err)
	}
	var docs []bson.D
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("sample %s: %w", coll, err)
	}
	ci := &colInfo{types: map[string]string{}}
	seen := map[string]map[string]bool{}
	add := func(n string) {
		if seen[n] == nil {
			seen[n] = map[string]bool{}
			ci.names = append(ci.names, n)
		}
	}
	add("_id")
	for _, d := range docs {
		for _, e := range d {
			add(e.Key)
			seen[e.Key][typeName(e.Value)] = true
		}
	}
	if len(docs) == 0 {
		seen["_id"]["objectId"] = true
	}
	for n, ts := range seen {
		if len(ts) > 1 {
			delete(ts, "null")
		}
		ci.types[n] = "null"
		switch len(ts) {
		case 0:
		case 1:
			for t := range ts {
				ci.types[n] = t
			}
		default:
			ci.types[n] = "mixed"
		}
	}
	s.mu.Lock()
	s.cache[dbn+"."+coll] = ci
	s.mu.Unlock()
	return ci, nil
}

func (s *session) cached(coll string) *colInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[s.cur+"."+coll]
}

func (s *session) info4(ctx context.Context, coll string) (*colInfo, error) {
	if ci := s.cached(coll); ci != nil {
		return ci, nil
	}
	return s.sample(ctx, coll)
}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	ci, err := s.sample(ctx, ref.Name)
	if err != nil {
		return nil, err
	}
	cols := make([]db.Column, len(ci.names))
	for i, n := range ci.names {
		cols[i] = db.Column{Name: n, OrigName: n, Type: ci.types[n], Nullable: n != "_id", PK: n == "_id"}
	}
	return cols, nil
}

func (s *session) listIndexes(ctx context.Context, dbn, coll string) ([]bson.D, error) {
	cur, err := s.client.Database(dbn).Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	var docs []bson.D
	return docs, cur.All(ctx, &docs)
}

func keysOf(v any) []string {
	d, _ := v.(bson.D)
	out := make([]string, len(d))
	for i, e := range d {
		out[i] = e.Key
	}
	return out
}

func (s *session) Indexes(ctx context.Context, ref db.ObjRef) ([]db.Index, error) {
	if ref.Kind == db.KindView {
		return nil, nil
	}
	docs, err := s.listIndexes(ctx, s.CurrentDatabase(), ref.Name)
	if err != nil {
		var ce mdb.CommandError
		if errors.As(err, &ce) && ce.Code == 26 { // NamespaceNotFound: empty/nonexistent collection
			return nil, nil
		}
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	var out []db.Index
	for _, d := range docs {
		name, _ := lookup(d, "name")
		key, _ := lookup(d, "key")
		ix := db.Index{Name: fmt.Sprint(name), Columns: keysOf(key), Algorithm: "btree"}
		ix.Primary = ix.Name == "_id_"
		if u, _ := lookup(d, "unique"); u == true {
			ix.Unique = true
		}
		for _, e := range key.(bson.D) {
			switch e.Value {
			case "text", "2dsphere", "2d", "hashed":
				ix.Algorithm = e.Value.(string)
			}
		}
		if w, ok := lookup(d, "weights"); ok && ix.Algorithm == "text" {
			ix.Columns = keysOf(w)
		}
		if p, ok := lookup(d, "partialFilterExpression"); ok {
			ix.Condition = extJSON(p)
		}
		out = append(out, ix)
	}
	return out, nil
}

// nameRef renders db.<coll> or db.getCollection("<coll>") when the name is not a plain identifier.
var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var dbMethods = map[string]bool{"getName": true, "getCollection": true, "getSiblingDB": true, "getCollectionNames": true,
	"createCollection": true, "createView": true, "dropDatabase": true, "runCommand": true, "adminCommand": true}

func collRef(name string) string {
	if plainName.MatchString(name) && !dbMethods[name] {
		return "db." + name
	}
	return "db.getCollection(" + jsonString(name) + ")"
}

func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	dbn := s.CurrentDatabase()
	specs, err := s.client.Database(dbn).ListCollectionSpecifications(ctx, bson.D{{Key: "name", Value: ref.Name}})
	if err != nil {
		return "", fmt.Errorf("collection options: %w", err)
	}
	if len(specs) == 0 {
		return "", fmt.Errorf("collection %q not found in %s", ref.Name, dbn)
	}
	var opts bson.D
	if len(specs[0].Options) > 0 {
		if err := bson.Unmarshal(specs[0].Options, &opts); err != nil {
			return "", err
		}
	}
	if specs[0].Type == "view" {
		on, _ := lookup(opts, "viewOn")
		pipe, ok := lookup(opts, "pipeline")
		if !ok {
			pipe = bson.A{}
		}
		return fmt.Sprintf("db.createView(%s, %s, %s)", jsonString(ref.Name), jsLit(on), jsLit(pipe)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "// collection %s, options: %s\n", ref.Name, jsLit(opts))
	if len(opts) > 0 {
		fmt.Fprintf(&b, "db.createCollection(%s, %s)\n", jsonString(ref.Name), jsLit(opts))
	}
	docs, err := s.listIndexes(ctx, dbn, ref.Name)
	if err != nil {
		return "", fmt.Errorf("list indexes: %w", err)
	}
	for _, d := range docs {
		name, _ := lookup(d, "name")
		if name == "_id_" {
			continue
		}
		key, _ := lookup(d, "key")
		o := bson.D{{Key: "name", Value: name}}
		for _, e := range d {
			if e.Key != "v" && e.Key != "key" && e.Key != "name" && e.Key != "ns" {
				o = append(o, e)
			}
		}
		fmt.Fprintf(&b, "%s.createIndex(%s, %s)\n", collRef(ref.Name), jsLit(key), jsLit(o))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	switch kind {
	case db.ScriptCreate:
		return s.DDL(ctx, ref)
	case db.ScriptDrop:
		return collRef(ref.Name) + ".drop()", nil
	case db.ScriptTruncate:
		return collRef(ref.Name) + ".deleteMany({})", nil
	case db.ScriptSelectTop:
		return collRef(ref.Name) + ".find({}).limit(100)", nil
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}

func (s *session) rawFilter(ctx context.Context, text string) (bson.D, error) {
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), false, &d); err == nil {
		return d, nil
	}
	// Relaxed shell syntax ({status: 'paid', _id: ObjectId('...')}) is evaluated, so refuse anything
	// that could run code: this text comes from the filter bar and must never have side effects.
	if !db.MongoPlainDocument(text) {
		return nil, errors.New("not a plain document: only values and BSON helpers like ObjectId() are allowed here")
	}
	sh, err := s.newShell(ctx)
	if err != nil {
		return nil, err
	}
	defer sh.Close()
	f, err := sh.eval("(" + text + ")")
	if err != nil {
		return nil, err
	}
	d, ok := f.V.(bson.D)
	if !ok {
		return nil, errors.New("not a document")
	}
	return d, nil
}

func (s *session) Rows(ctx context.Context, q db.RowQuery) (*db.Result, error) {
	ci, err := s.info4(ctx, q.Name)
	if err != nil {
		return nil, err
	}
	filter, err := buildFilter(q.Filters, ci, func(t string) (bson.D, error) { return s.rawFilter(ctx, t) })
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSkip(int64(q.Offset))
	if q.Limit > 0 {
		opts.SetLimit(int64(q.Limit))
	}
	if len(q.Sort) > 0 {
		sort := make(bson.D, len(q.Sort))
		for i, so := range q.Sort {
			sort[i] = bson.E{Key: so.Column, Value: 1}
			if so.Desc {
				sort[i].Value = -1
			}
		}
		opts.SetSort(sort)
	}
	cur, err := s.client.Database(s.CurrentDatabase()).Collection(q.Name).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", q.Name, err)
	}
	var docs []bson.D
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("find %s: %w", q.Name, err)
	}
	return render(docs, ci.names, ci.types), nil
}

func (s *session) Count(ctx context.Context, q db.RowQuery, estimate bool) (db.Count, error) {
	c := s.client.Database(s.CurrentDatabase()).Collection(q.Name)
	if len(q.Filters.Active()) == 0 {
		if estimate {
			if n, err := c.EstimatedDocumentCount(ctx); err == nil {
				return db.Count{N: n}, nil
			}
		}
		n, err := c.CountDocuments(ctx, bson.D{})
		return db.Count{N: n, Exact: true}, err
	}
	ci, err := s.info4(ctx, q.Name)
	if err != nil {
		return db.Count{}, err
	}
	filter, err := buildFilter(q.Filters, ci, func(t string) (bson.D, error) { return s.rawFilter(ctx, t) })
	if err != nil {
		return db.Count{}, err
	}
	n, err := c.CountDocuments(ctx, filter)
	if err != nil {
		return db.Count{}, fmt.Errorf("count %s: %w", q.Name, err)
	}
	return db.Count{N: n, Exact: true}, nil
}

// Exec runs a raw `{...}` command or Mongo-shell JavaScript.
func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	start := time.Now()
	t := strings.TrimSpace(stmt)
	var res *db.Result
	var err error
	if strings.HasPrefix(t, "{") {
		res, err = s.execCommand(ctx, t, limit)
	} else {
		res, err = s.execShell(ctx, t, limit)
	}
	if res != nil {
		res.Duration = time.Since(start)
	}
	return res, err
}

func (s *session) execCommand(ctx context.Context, text string, limit int) (*db.Result, error) {
	cmd, err := s.rawFilter(ctx, strings.TrimSuffix(text, ";"))
	if err != nil {
		return nil, fmt.Errorf("command: %w", err)
	}
	var out bson.D
	if err := s.client.Database(s.CurrentDatabase()).RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, err
	}
	docs := []bson.D{out}
	trunc := false
	if fb, ok := lookup(out, "cursor.firstBatch"); ok {
		docs = nil
		arr, _ := fb.(bson.A)
		for _, e := range arr {
			if d, ok := e.(bson.D); ok {
				docs = append(docs, d)
			}
		}
		if limit > 0 && len(docs) > limit {
			docs, trunc = docs[:limit], true
		}
	}
	if id, ok := lookup(out, "cursor.id"); ok { // a non-zero cursor id means the server has more batches
		switch v := id.(type) {
		case int64:
			trunc = trunc || v != 0
		case int32:
			trunc = trunc || v != 0
		}
	}
	r := render(docs, nil, nil)
	r.Truncated = trunc
	return r, nil
}

func (s *session) collectionExists(name string) (bool, error) {
	if s.client == nil { // planning without a server (unit tests): nothing to check
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	names, err := s.client.Database(s.CurrentDatabase()).ListCollectionNames(ctx, bson.D{{Key: "name", Value: name}})
	return len(names) > 0, err
}

// Plan writes shell statements; Apply runs them through the same evaluator.
func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	var out []db.Statement
	for _, c := range changes {
		var types map[string]string
		if ci := s.cached(c.Name); ci != nil {
			types = ci.types
		}
		key := bson.D{}
		for _, kv := range c.Key {
			v, err := coerceSet(kv.Val, types[kv.Col], kv.Col)
			if err != nil {
				return nil, err
			}
			key = append(key, bson.E{Key: kv.Col, Value: v})
		}
		set, unset := bson.D{}, bson.D{}
		for _, kv := range c.Set {
			if kv.Val == db.Default {
				unset = append(unset, bson.E{Key: kv.Col, Value: ""})
				continue
			}
			v, err := coerceSet(kv.Val, types[kv.Col], kv.Col)
			if err != nil {
				return nil, err
			}
			set = append(set, bson.E{Key: kv.Col, Value: v})
		}
		coll := collRef(c.Name)
		var text string
		switch c.Kind {
		case db.Insert:
			if _, has := lookup(set, "_id"); !has {
				set = append(bson.D{{Key: "_id", Value: bson.NewObjectID()}}, set...)
			}
			text = coll + ".insertOne(" + jsLit(set) + ")"
		case db.Update:
			if len(key) == 0 {
				return nil, fmt.Errorf("cannot update %s: no key to identify the document", c.Name)
			}
			if len(set) == 0 && len(unset) == 0 {
				return nil, errors.New("update without changed fields")
			}
			upd := bson.D{}
			if len(set) > 0 {
				upd = append(upd, bson.E{Key: "$set", Value: set})
			}
			if len(unset) > 0 {
				upd = append(upd, bson.E{Key: "$unset", Value: unset})
			}
			text = coll + ".updateOne(" + jsLit(key) + ", " + jsLit(upd) + ")"
		case db.Delete:
			if len(key) == 0 {
				return nil, fmt.Errorf("cannot delete from %s: no key to identify the document", c.Name)
			}
			text = coll + ".deleteOne(" + jsLit(key) + ")"
		default:
			return nil, fmt.Errorf("unknown change kind %d", c.Kind)
		}
		out = append(out, db.Statement{Text: text, Display: text})
	}
	return out, nil
}

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	var t string
	newName := opts["new_name"]
	switch op {
	case db.OpDropObject:
		t = collRef(ref.Name) + ".drop()"
	case db.OpTruncate:
		t = collRef(ref.Name) + ".deleteMany({})"
	case db.OpClone, db.OpRenameObject:
		if newName == "" {
			return nil, errors.New("new_name is required")
		}
		// $out would silently replace an existing collection: refuse instead of losing its data
		if exists, err := s.collectionExists(newName); err != nil {
			return nil, err
		} else if exists {
			return nil, fmt.Errorf("collection %q already exists", newName)
		}
		switch {
		case op == db.OpRenameObject:
			t = collRef(ref.Name) + ".renameCollection(" + jsonString(newName) + ")"
		case opts["with_data"] == "true":
			t = collRef(ref.Name) + `.aggregate([{"$match": {}}, {"$out": ` + jsonString(newName) + `}])`
		default:
			t = "db.createCollection(" + jsonString(newName) + ")"
		}
	case db.OpCreateDatabase:
		if opts["collection"] == "" {
			return nil, errors.New("MongoDB creates databases lazily: give the first collection name (opts collection)")
		}
		t = "db.getSiblingDB(" + jsonString(ref.Name) + ").createCollection(" + jsonString(opts["collection"]) + ")"
	case db.OpDropDatabase:
		t = "db.getSiblingDB(" + jsonString(ref.Name) + ").dropDatabase()"
	default:
		return nil, fmt.Errorf("%s is not supported by MongoDB", op)
	}
	return []db.Statement{{Text: t, Display: t}}, nil
}

func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	for _, st := range stmts {
		if _, err := s.Exec(ctx, st.Text, 0); err != nil {
			return fmt.Errorf("%s: %w", st.Shown(), err)
		}
	}
	return nil
}
