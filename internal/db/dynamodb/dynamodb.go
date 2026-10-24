// Package dynamodb is the Amazon DynamoDB driver (aws-sdk-go-v2).
//
// Connection mapping. DynamoDB has no logins, hosts or databases, so the connection fields are reused:
//
//	URL                  dynamodb://[ACCESSKEY:SECRET@]host:port/?region=us-east-1
//	Host/Port            empty = the real AWS endpoint for the region; set = a custom endpoint such as
//	                     DynamoDB Local (http unless ssl_mode is set or Options scheme=https)
//	User / Password      access key id / secret access key; both empty = the default AWS credential
//	                     chain (environment, shared config, SSO, instance role)
//	Database             the region (a "database" is a region here; Options region wins when both are set)
//	Options              region, profile (shared-config profile), session_token (temporary credentials),
//	                     endpoint (full URL, overrides Host/Port), scheme (http|https)
//	SSLMode/SSLCA/...    TLS settings for a custom https endpoint (the AWS endpoint always uses TLS)
//
// Regions are the "databases": Databases lists the AWS regions, UseDatabase re-points the client at
// another region, and creating, dropping or renaming one is refused.
//
// Tables are the only objects. A table is browsed with Query when the filters pin the partition key with
// "=" (plus at most one sort-key condition) and with Scan otherwise; the filter bar is turned into a
// native FilterExpression. DynamoDB cannot sort arbitrary columns: a Query sorts by the table's sort key
// on the server (ScanIndexForward), every other sort orders only the rows of the fetched page. There is
// no OFFSET: it is emulated by reading and discarding the first Offset items, which costs the read
// capacity of everything skipped. Items are schemaless, so the grid columns are the key attributes plus
// the attributes seen in the first 100 items and in the pages fetched since.
//
// SQL editor: PartiQL (ExecuteStatement, NextToken pagination). Pending edits are PartiQL statements
// with bound parameters (so numbers, binary, sets and nested documents keep their DynamoDB type) and are
// applied with ExecuteTransaction (atomic, at most 100 statements, refused beyond that). Table-level
// operations that PartiQL cannot express use readable statements the driver parses back itself:
//
//	CREATE TABLE "t" {json}             DROP TABLE "t"            TRUNCATE TABLE "t"
//	CREATE TABLE "copy" LIKE "t" [WITH DATA]
//	ALTER TABLE "t" ADD INDEX {json}    ALTER TABLE "t" DROP INDEX "name"
//
// They also run from the SQL editor. They are not atomic, and a table that is being created or changed is
// waited on until it is ACTIVE.
package dynamodb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"

	"tuidb/internal/config"
	"tuidb/internal/db"
)

func init() { db.Register(drv{}) }

type drv struct{}

func (drv) Name() string { return config.DynamoDB }

type session struct {
	cfg      aws.Config
	endpoint string // "" = AWS
	info     string

	mu     sync.Mutex
	region string
	cl     *dynamodb.Client
	metas  map[string]*tableMeta // table -> key schema and sampled attributes, per region
}

var regionRe = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d+$`)

// regions is a static list: listing them needs ec2:DescribeRegions, which a DynamoDB-only login lacks.
var regions = strings.Fields(`af-south-1 ap-east-1 ap-northeast-1 ap-northeast-2 ap-northeast-3 ap-south-1 ap-south-2
	ap-southeast-1 ap-southeast-2 ap-southeast-3 ap-southeast-4 ca-central-1 eu-central-1 eu-central-2 eu-north-1 eu-south-1
	eu-south-2 eu-west-1 eu-west-2 eu-west-3 il-central-1 me-central-1 me-south-1 sa-east-1 us-east-1 us-east-2 us-west-1 us-west-2`)

func tlsConfig(c config.Connection, host string) (*tls.Config, error) {
	switch c.SSLMode {
	case "", "disable", "prefer":
		return nil, nil
	case "require", "verify-ca", "verify-full", "verify-identity":
	default:
		return nil, fmt.Errorf("unsupported ssl_mode %q for dynamodb", c.SSLMode)
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
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

// endpointOf resolves the custom endpoint ("" = the AWS endpoint of the region).
func endpointOf(c config.Connection) (string, error) {
	raw := c.Options["endpoint"]
	if raw == "" {
		if c.Host == "" {
			return "", nil
		}
		scheme := c.Options["scheme"]
		if scheme == "" {
			scheme = "http"
			if c.SSLMode != "" && c.SSLMode != "disable" {
				scheme = "https"
			}
		}
		raw = scheme + "://" + c.Host
		if c.Port != 0 {
			raw = scheme + "://" + net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("endpoint: want http(s)://host[:port]")
	}
	if u.User != nil {
		return "", errors.New("endpoint must not contain credentials: use the user and password fields")
	}
	return strings.TrimSuffix(raw, "/"), nil
}

func (drv) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	endpoint, err := endpointOf(c)
	if err != nil {
		return nil, err
	}
	region := c.Options["region"]
	if region == "" {
		region = c.Database
	}
	if region != "" && !regionRe.MatchString(region) {
		return nil, fmt.Errorf("region %q is not an AWS region name (like us-east-1)", region)
	}
	if (c.User == "") != (c.Password == "") {
		return nil, errors.New("give both the access key id (user) and the secret access key (password), or neither to use the default AWS credential chain")
	}
	host := c.Host
	if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	tc, err := tlsConfig(c, host)
	if err != nil {
		return nil, err
	}
	hc := awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
		if dial != nil {
			tr.DialContext = dial
		}
		if tc != nil {
			tr.TLSClientConfig = tc
		}
	})
	// throttling (ProvisionedThroughputExceeded and friends) is retried with exponential backoff, at most 5s apart;
	// the SDK logger would write over the terminal UI, so it is silenced
	retryer := func() aws.Retryer {
		return retry.AddWithMaxBackoffDelay(retry.NewStandard(func(o *retry.StandardOptions) { o.MaxAttempts = 8 }), 5*time.Second)
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithHTTPClient(hc), awsconfig.WithRetryer(retryer), awsconfig.WithAppID("tuidb"), awsconfig.WithLogger(logging.Nop{})}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if p := c.Options["profile"]; p != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(p))
	}
	if c.User != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.User, c.Password, c.Options["session_token"])))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	if cfg.Region == "" {
		if endpoint == "" {
			return nil, errors.New("no region: add ?region=us-east-1 (or set the Database field / AWS_REGION)")
		}
		cfg.Region = "us-east-1" // a custom endpoint such as DynamoDB Local does not care
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		if endpoint == "" {
			return nil, fmt.Errorf("no AWS credentials: give an access key and secret, or configure the default chain (AWS_ACCESS_KEY_ID, ~/.aws, SSO): %w", err)
		}
		cfg.Credentials = credentials.NewStaticCredentialsProvider("local", "local", "") // DynamoDB Local accepts anything
	}
	s := &session{cfg: cfg, endpoint: endpoint, region: cfg.Region, metas: map[string]*tableMeta{}}
	s.cl = s.newClient(cfg.Region)
	s.info = "Amazon DynamoDB"
	if endpoint != "" {
		s.info = "DynamoDB at " + endpoint
	}
	if err := s.Ping(ctx); err != nil {
		return nil, err
	}
	for _, b := range c.Bootstrap {
		if _, err := s.Exec(ctx, b, 0); err != nil {
			return nil, fmt.Errorf("bootstrap %q: %w", b, err)
		}
	}
	return s, nil
}

func (s *session) newClient(region string) *dynamodb.Client {
	cfg := s.cfg
	cfg.Region = region
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if s.endpoint != "" {
			o.BaseEndpoint = aws.String(s.endpoint)
			o.DisableValidateResponseChecksum = true // DynamoDB Local sends checksums the SDK rejects
		}
	})
}

func (s *session) client() *dynamodb.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cl
}

func (s *session) Caps() db.Capabilities {
	return db.Capabilities{Indexes: true, Transactions: true, Document: true, Editor: "sql", Dialect: "partiql",
		DatabaseNoun: "region", Types: []string{"S", "N", "B"}}
}

func (s *session) ServerInfo() string { return s.info }
func (s *session) Close() error       { return nil }

func (s *session) Ping(ctx context.Context) error {
	_, err := s.client().ListTables(ctx, &dynamodb.ListTablesInput{Limit: aws.Int32(1)})
	return wrap("ping", err)
}

func (s *session) CurrentDatabase() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.region
}

func (s *session) Databases(context.Context) ([]string, error) {
	cur := s.CurrentDatabase()
	out := append([]string(nil), regions...)
	if !contains(out, cur) {
		out = append(out, cur)
		sort.Strings(out)
	}
	return out, nil
}

func (s *session) UseDatabase(ctx context.Context, name string) error {
	if !regionRe.MatchString(name) {
		return fmt.Errorf("%q is not an AWS region name (like us-east-1)", name)
	}
	cl := s.newClient(name)
	if _, err := cl.ListTables(ctx, &dynamodb.ListTablesInput{Limit: aws.Int32(1)}); err != nil {
		return wrap("region "+name, err)
	}
	s.mu.Lock()
	s.region, s.cl, s.metas = name, cl, map[string]*tableMeta{}
	s.mu.Unlock()
	return nil
}

func (s *session) Schemas(context.Context) ([]string, error) { return nil, nil }
func (s *session) CurrentSchema() string                     { return "" }
func (s *session) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error) {
	return nil, nil
}
func (s *session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) {
	return nil, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// wrap shortens an AWS error to "op: Code: message" (the SDK's own text carries request ids and the
// operation chain); context errors pass through so cancellation is recognisable.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	switch { // the cause stays wrapped so errors.Is still sees the cancellation
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: %w", op, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: timed out (is the endpoint reachable?): %w", op, context.DeadlineExceeded)
	}
	var tc *types.TransactionCanceledException
	if errors.As(err, &tc) {
		var reasons []string
		for _, r := range tc.CancellationReasons {
			reasons = append(reasons, aws.ToString(r.Code))
		}
		return fmt.Errorf("%s: transaction cancelled, nothing was applied (%s): %s", op, strings.Join(reasons, ", "), aws.ToString(tc.Message))
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return fmt.Errorf("%s: %s: %s", op, ae.ErrorCode(), ae.ErrorMessage())
	}
	return fmt.Errorf("%s: %w", op, err)
}

// ---- table metadata -------------------------------------------------------------------

// tableMeta is what Rows, filters and Plan need to know about a table. attrs holds the type of every
// attribute seen so far: key types come from the schema, the rest from sampled items ("mixed" when an
// attribute holds different types in different items).
type tableMeta struct {
	name    string
	hash    string
	rng     string // "" = no sort key
	attrs   map[string]string
	names   []string            // column order: keys first, then the rest
	indexOf map[string][]string // attribute -> indexes whose key it is
}

func (m *tableMeta) keys() []string {
	if m.rng == "" {
		return []string{m.hash}
	}
	return []string{m.hash, m.rng}
}

func (m *tableMeta) isKey(a string) bool { return a == m.hash || (m.rng != "" && a == m.rng) }

func metaOf(d *types.TableDescription) *tableMeta {
	m := &tableMeta{name: aws.ToString(d.TableName), attrs: map[string]string{}, indexOf: map[string][]string{}}
	for _, k := range d.KeySchema {
		if k.KeyType == types.KeyTypeHash {
			m.hash = aws.ToString(k.AttributeName)
		} else {
			m.rng = aws.ToString(k.AttributeName)
		}
	}
	for _, a := range d.AttributeDefinitions {
		m.attrs[aws.ToString(a.AttributeName)] = string(a.AttributeType)
	}
	add := func(name *string, ks []types.KeySchemaElement) {
		for _, k := range ks {
			a := aws.ToString(k.AttributeName)
			if !m.isKey(a) {
				m.indexOf[a] = append(m.indexOf[a], aws.ToString(name))
			}
		}
	}
	for _, g := range d.GlobalSecondaryIndexes {
		add(g.IndexName, g.KeySchema)
	}
	for _, l := range d.LocalSecondaryIndexes {
		add(l.IndexName, l.KeySchema)
	}
	m.names = append(m.names, m.keys()...)
	var rest []string
	for a := range m.attrs {
		if !m.isKey(a) {
			rest = append(rest, a)
		}
	}
	sort.Strings(rest)
	m.names = append(m.names, rest...)
	return m
}

// learn records the attributes of items; new names are appended in alphabetical order. Caller holds s.mu.
func (m *tableMeta) learn(items []map[string]types.AttributeValue) {
	var fresh []string
	seen := map[string]bool{}
	for _, it := range items {
		for a, v := range it {
			t := typeName(v)
			cur, known := m.attrs[a]
			switch {
			case !known:
				m.attrs[a] = t
				if !seen[a] {
					fresh = append(fresh, a)
					seen[a] = true
				}
			case cur == t || m.isKey(a) || cur == "mixed":
			case cur == "NULL":
				m.attrs[a] = t
			case t != "NULL":
				m.attrs[a] = "mixed"
			}
		}
	}
	sort.Strings(fresh)
	for _, a := range fresh {
		if !contains(m.names, a) {
			m.names = append(m.names, a)
		}
	}
}

func (s *session) describe(ctx context.Context, table string) (*types.TableDescription, error) {
	out, err := s.client().DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)})
	if err != nil {
		return nil, wrap("describe table "+table, err)
	}
	return out.Table, nil
}

// loadMeta reads the key schema and samples the first 100 items.
func (s *session) loadMeta(ctx context.Context, table string) (*tableMeta, error) {
	d, err := s.describe(ctx, table)
	if err != nil {
		return nil, err
	}
	m := metaOf(d)
	out, err := s.client().Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(table), Limit: aws.Int32(100)})
	if err != nil {
		return nil, wrap("sample "+table, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m.learn(out.Items)
	s.metas[table] = m
	return m, nil
}

func (s *session) meta(ctx context.Context, table string) (*tableMeta, error) {
	s.mu.Lock()
	m := s.metas[table]
	s.mu.Unlock()
	if m != nil {
		return m, nil
	}
	return s.loadMeta(ctx, table)
}

func (s *session) forget() {
	s.mu.Lock()
	s.metas = map[string]*tableMeta{}
	s.mu.Unlock()
}

// ---- objects, columns, indexes, DDL -----------------------------------------------------

func humanBytes(n int64) string {
	const u = "KMGT"
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, i := float64(n)/1024, 0
	for f >= 1024 && i < len(u)-1 {
		f, i = f/1024, i+1
	}
	return fmt.Sprintf("%.1f %cB", f, u[i])
}

func (s *session) tableNames(ctx context.Context) ([]string, error) {
	var names []string
	var start *string
	for {
		out, err := s.client().ListTables(ctx, &dynamodb.ListTablesInput{ExclusiveStartTableName: start})
		if err != nil {
			return nil, wrap("list tables", err)
		}
		names = append(names, out.TableNames...)
		if out.LastEvaluatedTableName == nil {
			break
		}
		start = out.LastEvaluatedTableName
	}
	sort.Strings(names)
	return names, nil
}

// detailLimit caps the DescribeTable calls made to fill the sidebar's item counts.
const detailLimit = 100

func (s *session) Objects(ctx context.Context, _ string) ([]db.Object, error) {
	names, err := s.tableNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]db.Object, len(names))
	for i, n := range names {
		out[i] = db.Object{ObjRef: db.ObjRef{Name: n, Kind: db.KindTable}}
	}
	// DescribeTable counts are one round trip each: concurrent, bounded, and only for the first tables.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range out[:min(len(out), detailLimit)] {
		wg.Add(1)
		sem <- struct{}{}
		go func(o *db.Object) {
			defer func() { <-sem; wg.Done() }()
			if d, err := s.describe(ctx, o.Name); err == nil { // ItemCount is refreshed by AWS about every 6 hours
				o.Detail = fmt.Sprintf("%d items, %s", aws.ToInt64(d.ItemCount), humanBytes(aws.ToInt64(d.TableSizeBytes)))
			}
		}(&out[i])
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, nil
}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	m, err := s.loadMeta(ctx, ref.Name)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cols := make([]db.Column, len(m.names))
	for i, n := range m.names {
		c := db.Column{Name: n, OrigName: n, Type: m.attrs[n], Nullable: !m.isKey(n), PK: m.isKey(n)}
		switch {
		case n == m.hash:
			c.Extra = "partition key"
		case n == m.rng:
			c.Extra = "sort key"
		case len(m.indexOf[n]) > 0:
			c.Extra = "index key: " + strings.Join(m.indexOf[n], ", ")
		}
		cols[i] = c
	}
	return cols, nil
}

func projectionText(p *types.Projection) string {
	if p == nil {
		return ""
	}
	if p.ProjectionType == types.ProjectionTypeInclude {
		return "INCLUDE " + strings.Join(p.NonKeyAttributes, ", ")
	}
	return string(p.ProjectionType)
}

func keyNames(ks []types.KeySchemaElement) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = aws.ToString(k.AttributeName)
	}
	return out
}

func (s *session) Indexes(ctx context.Context, ref db.ObjRef) ([]db.Index, error) {
	d, err := s.describe(ctx, ref.Name)
	if err != nil {
		return nil, err
	}
	out := []db.Index{{Name: "PRIMARY", Columns: keyNames(d.KeySchema), Unique: true, Primary: true, Algorithm: "KEY"}}
	for _, g := range d.GlobalSecondaryIndexes {
		out = append(out, db.Index{Name: aws.ToString(g.IndexName), Columns: keyNames(g.KeySchema), Algorithm: "GSI", Condition: projectionText(g.Projection)})
	}
	for _, l := range d.LocalSecondaryIndexes {
		out = append(out, db.Index{Name: aws.ToString(l.IndexName), Columns: keyNames(l.KeySchema), Algorithm: "LSI", Condition: projectionText(l.Projection)})
	}
	return out, nil
}

// prune drops nulls, empty strings, zero times and empty collections so the DescribeTable dump only
// shows what is set.
func prune(v any) any {
	switch x := v.(type) {
	case string:
		if x == "" || x == "0001-01-01T00:00:00Z" || x == "1970-01-01T00:00:00Z" {
			return nil
		}
	case map[string]any:
		for k, e := range x {
			if e = prune(e); e == nil {
				delete(x, k)
			} else {
				x[k] = e
			}
		}
		if len(x) == 0 {
			return nil
		}
	case []any:
		if len(x) == 0 {
			return nil
		}
		for i := range x {
			x[i] = prune(x[i])
		}
	}
	return v
}

// DDL is an abridged `aws dynamodb describe-table`, plus TTL (a separate API) and the streams settings.
func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	d, err := s.describe(ctx, ref.Name)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	if t, err := s.client().DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{TableName: aws.String(ref.Name)}); err == nil && t.TimeToLiveDescription != nil {
		doc["TimeToLive"] = map[string]any{"Status": t.TimeToLiveDescription.TimeToLiveStatus, "AttributeName": aws.ToString(t.TimeToLiveDescription.AttributeName)}
	}
	if d.BillingModeSummary == nil { // tables created as PROVISIONED carry no summary
		doc["BillingMode"] = types.BillingModeProvisioned
	} else if d.BillingModeSummary.BillingMode == types.BillingModePayPerRequest {
		delete(doc, "ProvisionedThroughput") // all zeros on an on-demand table
		gsis, _ := doc["GlobalSecondaryIndexes"].([]any)
		for _, g := range gsis {
			if gm, ok := g.(map[string]any); ok {
				delete(gm, "ProvisionedThroughput")
			}
		}
	}
	if d.StreamSpecification == nil {
		doc["StreamSpecification"] = map[string]any{"StreamEnabled": false}
	}
	prune(doc)
	b, err := json.MarshalIndent(doc, "", "  ")
	return string(b), err
}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	q := quote(ref.Name)
	switch kind {
	case db.ScriptCreate:
		d, err := s.describe(ctx, ref.Name)
		if err != nil {
			return "", err
		}
		return createText(ref.Name, specOf(d)), nil
	case db.ScriptDrop:
		return "DROP TABLE " + q, nil
	case db.ScriptTruncate:
		return "TRUNCATE TABLE " + q, nil
	case db.ScriptSelectTop:
		return "SELECT * FROM " + q, nil // PartiQL has no LIMIT: the editor's row limit stops the read
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}
