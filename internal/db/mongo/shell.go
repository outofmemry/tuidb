package mongo

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/token"
	"go.mongodb.org/mongo-driver/v2/bson"
	mdb "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"tuidb/internal/db"
)

//go:embed shim.js
var shimSrc string

//go:embed values.js
var valuesSrc string

var (
	shimProg   = sync.OnceValues(func() (*goja.Program, error) { return goja.Compile("shim.js", shimSrc, false) })
	valuesProg = sync.OnceValues(func() (*goja.Program, error) { return goja.Compile("values.js", valuesSrc, false) })
)

// shortcut: toArray/forEach on a cursor stop at this many documents, raise it if people need more.
const maxScript = 50000

type shell struct {
	s    *session
	ctx  context.Context
	vm   *goja.Runtime
	fin  goja.Callable // __finish, captured before user code can replace it
	err  error         // first error raised by a native op
	aff  int64
	out  []string
	done chan struct{}
}

type curSpec struct {
	DB    string `bson:"db"`
	Coll  string `bson:"coll"`
	Kind  string `bson:"kind"`
	A     bson.A `bson:"a"`
	Sort  bson.D `bson:"sort"`
	Proj  any    `bson:"proj"`
	Limit int64  `bson:"limit"`
	Skip  int64  `bson:"skip"`
}

type finished struct {
	K string  `bson:"k"`
	S curSpec `bson:"s"`
	V any     `bson:"v"`
	M string  `bson:"m"`
}

// newShell prepares a goja runtime bound to the session's current database; Close stops its watcher.
func (s *session) newShell(ctx context.Context) (*shell, error) {
	vals, err := valuesProg()
	if err != nil {
		return nil, err
	}
	prog, err := shimProg()
	if err != nil {
		return nil, err
	}
	sh := &shell{s: s, ctx: ctx, vm: goja.New(), done: make(chan struct{})}
	sh.vm.Set("__op", sh.opNative)
	sh.vm.Set("__run", sh.runNative)
	sh.vm.Set("__oid", func() string { return bson.NewObjectID().Hex() })
	sh.vm.Set("__print", func(t string) { sh.out = append(sh.out, t) })
	sh.vm.Set("__db", s.CurrentDatabase())
	for _, p := range []*goja.Program{vals, prog} {
		if _, err := sh.vm.RunProgram(p); err != nil {
			return nil, err
		}
	}
	fin, ok := goja.AssertFunction(sh.vm.Get("__finish"))
	if !ok {
		return nil, errors.New("shell: __finish is not callable")
	}
	sh.fin = fin
	go func() {
		select {
		case <-ctx.Done():
			sh.vm.Interrupt(ctx.Err())
		case <-sh.done:
		}
	}()
	return sh, nil
}

func (sh *shell) Close() { close(sh.done) }

// eval runs JS and returns the __finish envelope for its completion value.
func (sh *shell) eval(src string) (_ *finished, err error) {
	defer func() { // a goja panic must fail this statement, not kill the TUI
		if r := recover(); r != nil {
			err = fmt.Errorf("shell: %v", r)
		}
	}()
	v, err := sh.vm.RunString(src)
	if err == nil {
		v, err = sh.fin(goja.Undefined(), v)
	}
	if err != nil {
		if sh.err != nil {
			return nil, sh.err
		}
		if sh.ctx.Err() != nil {
			return nil, sh.ctx.Err()
		}
		var ex *goja.Exception
		if errors.As(err, &ex) {
			return nil, errors.New(ex.Value().String())
		}
		return nil, err
	}
	var f finished
	if err := bson.UnmarshalExtJSON([]byte(v.String()), false, &f); err != nil {
		return nil, fmt.Errorf("shell result: %w", err)
	}
	return &f, nil
}

// valueHelpers are the only functions shell-syntax documents may call (Date and RegExp also with new).
var valueHelpers = map[string]bool{"ObjectId": true, "ISODate": true, "NumberLong": true, "NumberInt": true, "NumberDecimal": true,
	"Date": true, "RegExp": true, "UUID": true, "BinData": true, "Timestamp": true, "MinKey": true, "MaxKey": true}

// plainValue reports whether e is literal data: literals, object/array literals and value-helper calls.
// Anything else (identifiers, member access, templates, functions, assignments...) is rejected.
func plainValue(e ast.Expression) bool {
	switch x := e.(type) {
	case nil: // array hole
		return true
	case *ast.StringLiteral, *ast.NumberLiteral, *ast.BooleanLiteral, *ast.NullLiteral, *ast.RegExpLiteral:
		return true
	case *ast.UnaryExpression:
		return !x.Postfix && (x.Operator == token.MINUS || x.Operator == token.PLUS) && plainValue(x.Operand)
	case *ast.ArrayLiteral:
		return allPlain(x.Value)
	case *ast.ObjectLiteral:
		for _, p := range x.Value {
			k, ok := p.(*ast.PropertyKeyed)
			if !ok || k.Computed || k.Kind != ast.PropertyKindValue || !plainValue(k.Value) {
				return false
			}
		}
		return true
	case *ast.CallExpression:
		return helperCall(x.Callee, x.ArgumentList)
	case *ast.NewExpression:
		return helperCall(x.Callee, x.ArgumentList)
	}
	return false
}

func allPlain(es []ast.Expression) bool {
	for _, e := range es {
		if !plainValue(e) {
			return false
		}
	}
	return true
}

func helperCall(callee ast.Expression, args []ast.Expression) bool {
	id, ok := callee.(*ast.Identifier)
	return ok && valueHelpers[id.Name.String()] && allPlain(args)
}

var errNotPlain = errors.New("not a plain document: only values and BSON helpers like ObjectId() are allowed here")

// parseDoc reads a document written as extended JSON or in shell syntax ({status: 'paid', _id: ObjectId('...')}).
// This text comes from the filter bar and must never have side effects, so shell syntax must be literal data
// (checked on the parsed AST) and is evaluated in a fresh runtime that has only the BSON value helpers.
func parseDoc(text string) (bson.D, error) {
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), false, &d); err == nil {
		return d, nil
	}
	if !db.MongoPlainDocument(text) { // cheap first check; the AST check below is the real gate
		return nil, errNotPlain
	}
	prg, err := goja.Parse("document", "("+text+"\n)")
	if err != nil {
		return nil, err
	}
	if len(prg.Body) != 1 {
		return nil, errNotPlain
	}
	st, ok := prg.Body[0].(*ast.ExpressionStatement)
	if !ok || !plainValue(st.Expression) {
		return nil, errNotPlain
	}
	if _, ok := st.Expression.(*ast.ObjectLiteral); !ok {
		return nil, errors.New("not a document")
	}
	vals, err := valuesProg()
	if err != nil {
		return nil, err
	}
	p, err := goja.CompileAST(prg, false)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	if _, err := vm.RunProgram(vals); err != nil {
		return nil, err
	}
	ser, ok := goja.AssertFunction(vm.Get("__ser"))
	if !ok {
		return nil, errors.New("__ser is not callable")
	}
	v, err := vm.RunProgram(p)
	if err == nil {
		v, err = ser(goja.Undefined(), v)
	}
	if err != nil {
		return nil, err
	}
	if jv, ok := parseJSONValue(v.String()); ok {
		if d, ok := jv.(bson.D); ok {
			return d, nil
		}
	}
	return nil, errors.New("not a document")
}

func (sh *shell) fail(err error) error {
	if sh.err == nil {
		sh.err = err
	}
	return err
}

func (sh *shell) opNative(dbn, coll, op, argsJSON string) (string, error) {
	var w struct {
		A bson.A `bson:"a"`
	}
	if err := bson.UnmarshalExtJSON([]byte(argsJSON), false, &w); err != nil {
		return "", sh.fail(fmt.Errorf("bad arguments: %w", err))
	}
	v, msg, err := sh.dispatch(dbn, coll, op, argv(w.A))
	if err != nil {
		return "", sh.fail(fmt.Errorf("%s: %w", op, err))
	}
	b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}, {Key: "m", Value: msg}}, false, false)
	return string(b), err
}

func (sh *shell) runNative(specJSON string) (string, error) {
	var sp curSpec
	if err := bson.UnmarshalExtJSON([]byte(specJSON), false, &sp); err != nil {
		return "", sh.fail(err)
	}
	docs, _, err := sh.s.runCursor(sh.ctx, sp, maxScript)
	if err != nil {
		return "", sh.fail(fmt.Errorf("%s: %w", sp.Kind, err))
	}
	arr := make(bson.A, len(docs))
	for i, d := range docs {
		arr[i] = d
	}
	b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: arr}}, false, false)
	return string(b), err
}

type argv bson.A

func (a argv) at(i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

func (a argv) doc(i int) (bson.D, error) {
	switch x := a.at(i).(type) {
	case nil:
		return bson.D{}, nil
	case bson.D:
		return x, nil
	}
	return nil, fmt.Errorf("argument %d must be a document", i+1)
}

func (a argv) str(i int) (string, error) {
	if s, ok := a.at(i).(string); ok {
		return s, nil
	}
	return "", fmt.Errorf("argument %d must be a string", i+1)
}

func ok1(extra ...bson.E) bson.D { return append(bson.D{{Key: "acknowledged", Value: true}}, extra...) }

func (sh *shell) dispatch(dbn, coll, op string, a argv) (any, string, error) {
	ctx, cl := sh.ctx, sh.s.client
	c := cl.Database(dbn).Collection(coll)
	switch op {
	case "listDatabaseNames":
		names, err := sh.s.databases(ctx)
		return strs(names), "", err
	case "listCollectionNames":
		specs, err := cl.Database(dbn).ListCollectionSpecifications(ctx, bson.D{})
		var names []string
		for _, sp := range specs {
			names = append(names, sp.Name)
		}
		sort.Strings(names)
		return strs(names), "", err
	case "createCollection":
		cmd := bson.D{{Key: "create", Value: coll}}
		if o, _ := a.doc(0); o != nil {
			cmd = append(cmd, o...)
		}
		_, err := sh.s.runCmd(ctx, dbn, cmd)
		return ok1(), "created collection " + coll, err
	case "createView":
		src, err := a.str(0)
		if err != nil {
			return nil, "", err
		}
		cmd := bson.D{{Key: "create", Value: coll}, {Key: "viewOn", Value: src}, {Key: "pipeline", Value: a.at(1)}}
		if o, _ := a.doc(2); o != nil {
			cmd = append(cmd, o...)
		}
		_, err = sh.s.runCmd(ctx, dbn, cmd)
		return ok1(), "created view " + coll, err
	case "dropDatabase":
		err := cl.Database(dbn).Drop(ctx)
		return ok1(bson.E{Key: "dropped", Value: dbn}), "dropped database " + dbn, err
	case "runCommand":
		cmd, err := a.doc(0)
		if err != nil {
			return nil, "", err
		}
		out, err := sh.s.runCmd(ctx, dbn, cmd)
		return out, "", err
	case "findOne":
		f, err := a.doc(0)
		if err != nil {
			return nil, "", err
		}
		opts := options.Find().SetLimit(1)
		if p := a.at(1); p != nil {
			opts.SetProjection(p)
		}
		cur, err := c.Find(ctx, f, opts)
		if err != nil {
			return nil, "", err
		}
		var docs []bson.D
		if err := cur.All(ctx, &docs); err != nil || len(docs) == 0 {
			return nil, "", err
		}
		return docs[0], "", nil
	case "countDocuments":
		f, err := a.doc(0)
		if err != nil {
			return nil, "", err
		}
		n, err := c.CountDocuments(ctx, f)
		return n, "", err
	case "estimatedDocumentCount":
		n, err := c.EstimatedDocumentCount(ctx)
		return n, "", err
	case "distinct":
		field, err := a.str(0)
		if err != nil {
			return nil, "", err
		}
		f, err := a.doc(1)
		if err != nil {
			return nil, "", err
		}
		var vals bson.A
		err = c.Distinct(ctx, field, f).Decode(&vals)
		return vals, "", err
	case "insertOne":
		d, err := a.doc(0)
		if err != nil {
			return nil, "", err
		}
		r, err := c.InsertOne(ctx, d)
		if err != nil {
			return nil, "", err
		}
		sh.aff++
		return ok1(bson.E{Key: "insertedId", Value: r.InsertedID}), fmt.Sprintf("insertedId: %v", cell(r.InsertedID)), nil
	case "insertMany":
		docs, _ := a.at(0).(bson.A)
		if len(docs) == 0 {
			return nil, "", errors.New("argument 1 must be a non-empty array of documents")
		}
		r, err := c.InsertMany(ctx, []any(docs))
		if err != nil {
			return nil, "", err
		}
		sh.aff += int64(len(r.InsertedIDs))
		ids := make(bson.A, len(r.InsertedIDs))
		for i, id := range r.InsertedIDs {
			ids[i] = id
		}
		return ok1(bson.E{Key: "insertedIds", Value: ids}), fmt.Sprintf("inserted %d documents", len(ids)), nil
	case "updateOne", "updateMany", "replaceOne":
		return sh.update(dbn, coll, op, a)
	case "deleteOne", "deleteMany":
		f, err := a.doc(0)
		if err != nil {
			return nil, "", err
		}
		limit := int32(0)
		if op == "deleteOne" {
			limit = 1
		}
		out, err := sh.s.runCmd(ctx, dbn, bson.D{{Key: "delete", Value: coll}, {Key: "deletes", Value: bson.A{bson.D{{Key: "q", Value: f}, {Key: "limit", Value: limit}}}}})
		if err != nil {
			return nil, "", err
		}
		n, _ := lookup(out, "n")
		sh.aff += toInt(n)
		return ok1(bson.E{Key: "deletedCount", Value: toInt(n)}), fmt.Sprintf("deleted %d", toInt(n)), nil
	case "createIndex":
		keys, err := a.doc(0)
		if err != nil || len(keys) == 0 {
			return nil, "", errors.New("createIndex needs a key document")
		}
		o, _ := a.doc(1)
		spec := bson.D{{Key: "key", Value: keys}}
		if _, has := lookup(o, "name"); !has {
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s_%v", k.Key, k.Value))
			}
			spec = append(spec, bson.E{Key: "name", Value: strings.Join(parts, "_")})
		}
		spec = append(spec, o...)
		_, err = sh.s.runCmd(ctx, dbn, bson.D{{Key: "createIndexes", Value: coll}, {Key: "indexes", Value: bson.A{spec}}})
		name, _ := lookup(spec, "name")
		return ok1(bson.E{Key: "name", Value: name}), fmt.Sprintf("created index %v", name), err
	case "dropIndex":
		_, err := sh.s.runCmd(ctx, dbn, bson.D{{Key: "dropIndexes", Value: coll}, {Key: "index", Value: a.at(0)}})
		return ok1(), "dropped index", err
	case "getIndexes":
		docs, err := sh.s.listIndexes(ctx, dbn, coll)
		arr := make(bson.A, len(docs))
		for i, d := range docs {
			arr[i] = d
		}
		return arr, "", err
	case "drop":
		return ok1(bson.E{Key: "dropped", Value: coll}), "dropped collection " + coll, c.Drop(ctx)
	case "renameCollection":
		to, err := a.str(0)
		if err != nil {
			return nil, "", err
		}
		_, err = sh.s.runCmd(ctx, "admin", bson.D{{Key: "renameCollection", Value: dbn + "." + coll}, {Key: "to", Value: dbn + "." + to}})
		return ok1(), "renamed " + coll + " to " + to, err
	}
	return nil, "", fmt.Errorf("unsupported operation")
}

func strs(ss []string) bson.A {
	out := make(bson.A, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (sh *shell) update(dbn, coll, op string, a argv) (any, string, error) {
	f, err := a.doc(0)
	if err != nil {
		return nil, "", err
	}
	if a.at(1) == nil {
		return nil, "", errors.New("argument 2 (update) is required")
	}
	o, _ := a.doc(2)
	u := bson.D{{Key: "q", Value: f}, {Key: "u", Value: a.at(1)}, {Key: "multi", Value: op == "updateMany"}}
	for _, e := range o {
		switch e.Key {
		case "upsert", "arrayFilters", "collation", "hint":
			u = append(u, e)
		}
	}
	out, err := sh.s.runCmd(sh.ctx, dbn, bson.D{{Key: "update", Value: coll}, {Key: "updates", Value: bson.A{u}}})
	if err != nil {
		return nil, "", err
	}
	n, _ := lookup(out, "n")
	mod, _ := lookup(out, "nModified")
	up, _ := lookup(out, "upserted")
	ups, _ := up.(bson.A)
	matched := toInt(n) - int64(len(ups))
	res := ok1(bson.E{Key: "matchedCount", Value: matched}, bson.E{Key: "modifiedCount", Value: toInt(mod)})
	msg := fmt.Sprintf("matched %d, modified %d", matched, toInt(mod))
	sh.aff += toInt(mod) + int64(len(ups))
	if len(ups) > 0 {
		id, _ := lookup(ups[0].(bson.D), "_id")
		res = append(res, bson.E{Key: "upsertedId", Value: id})
		msg += fmt.Sprintf(", upsertedId: %v", cell(id))
	}
	return res, msg, nil
}

var (
	reUse  = regexp.MustCompile(`(?is)^use\s+(\S+?)\s*;?\s*$`)
	reShow = regexp.MustCompile(`(?is)^show\s+(dbs|databases|collections|tables)\s*;?\s*$`)
)

// execShell handles `use`, `show ...` and Mongo-shell JavaScript.
func (s *session) execShell(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	if m := reUse.FindStringSubmatch(stmt); m != nil {
		name := strings.Trim(m[1], `"'`)
		if err := s.UseDatabase(ctx, name); err != nil {
			return nil, err
		}
		return &db.Result{Message: "switched to db " + name}, nil
	}
	if m := reShow.FindStringSubmatch(stmt); m != nil {
		stmt = `__named(db.getCollectionNames(), "collection")`
		if k := strings.ToLower(m[1]); k == "dbs" || k == "databases" {
			stmt = `__named(__call("", "", "listDatabaseNames", []), "database")`
		}
	}
	sh, err := s.newShell(ctx)
	if err != nil {
		return nil, fmt.Errorf("shell: %w", err)
	}
	defer sh.Close()
	f, err := sh.eval(stmt)
	if err != nil {
		return nil, err
	}
	switch f.K {
	case "c":
		docs, trunc, err := s.runCursor(ctx, f.S, limit)
		if err != nil {
			return nil, err
		}
		r := render(docs, nil, nil)
		r.Truncated = trunc
		return r, nil
	case "u":
		msg := strings.Join(sh.out, "\n")
		if msg == "" {
			msg = "OK"
		}
		return &db.Result{Message: msg}, nil
	}
	if f.M != "" {
		return &db.Result{Message: f.M, Affected: sh.aff}, nil
	}
	var docs []bson.D
	switch v := f.V.(type) {
	case bson.D:
		docs = []bson.D{v}
	case bson.A:
		for _, e := range v {
			if d, ok := e.(bson.D); ok {
				docs = append(docs, d)
			} else {
				docs = append(docs, bson.D{{Key: "value", Value: e}})
			}
		}
	default:
		docs = []bson.D{{{Key: "value", Value: v}}}
	}
	trunc := limit > 0 && len(docs) > limit
	if trunc {
		docs = docs[:limit]
	}
	r := render(docs, nil, nil)
	r.Truncated = trunc
	return r, nil
}

// runCursor executes a find/aggregate spec; max>0 caps the rows (one extra is read to flag truncation).
func (s *session) runCursor(ctx context.Context, sp curSpec, max int) ([]bson.D, bool, error) {
	eff := sp.Limit
	if eff < 0 {
		eff = -eff
	}
	if max > 0 && (eff == 0 || eff > int64(max)+1) {
		eff = int64(max) + 1
	}
	c := s.client.Database(sp.DB).Collection(sp.Coll)
	var arg0 any
	if len(sp.A) > 0 {
		arg0 = sp.A[0]
	}
	var cur *mdb.Cursor
	var err error
	if sp.Kind == "aggregate" {
		if arg0 == nil {
			arg0 = bson.A{}
		}
		// cursor .sort/.skip become trailing stages (sort first, as in mongosh); the read loop applies the limit,
		// so a pipeline ending in $out is untouched unless the user chained them
		if pipe, ok := arg0.(bson.A); ok {
			if len(sp.Sort) > 0 {
				pipe = append(pipe, bson.D{{Key: "$sort", Value: sp.Sort}})
			}
			if sp.Skip > 0 {
				pipe = append(pipe, bson.D{{Key: "$skip", Value: sp.Skip}})
			}
			arg0 = pipe
		}
		cur, err = c.Aggregate(ctx, arg0)
	} else {
		if arg0 == nil {
			arg0 = bson.D{}
		}
		opts := options.Find().SetSkip(sp.Skip)
		if eff > 0 {
			opts.SetLimit(eff)
		}
		if len(sp.Sort) > 0 {
			opts.SetSort(sp.Sort)
		}
		proj := sp.Proj
		if proj == nil && len(sp.A) > 1 {
			proj = sp.A[1]
		}
		if proj != nil {
			opts.SetProjection(proj)
		}
		cur, err = c.Find(ctx, arg0, opts)
	}
	if err != nil {
		return nil, false, err
	}
	defer cur.Close(context.WithoutCancel(ctx))
	var docs []bson.D
	for cur.Next(ctx) {
		var d bson.D
		if err := cur.Decode(&d); err != nil {
			return nil, false, err
		}
		docs = append(docs, d)
		if eff > 0 && int64(len(docs)) >= eff {
			break
		}
	}
	if err := cur.Err(); err != nil {
		return nil, false, err
	}
	trunc := max > 0 && len(docs) > max
	if trunc {
		docs = docs[:max]
	}
	return docs, trunc, nil
}
