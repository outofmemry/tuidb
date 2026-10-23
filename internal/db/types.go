// Package db is the engine-neutral contract between the UI and the five drivers
// (postgres, mysql, clickhouse, redis, mongodb). The UI only talks to Session and
// consults Capabilities to hide what an engine lacks.
package db

import (
	"context"
	"net"
	"time"

	"webtuidb/internal/config"
)

// DialFunc lets drivers connect through an SSH tunnel. Nil means net.Dialer.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Driver opens sessions. conn.Password is already resolved (password_cmd run by the caller).
type Driver interface {
	Name() string
	Open(ctx context.Context, conn config.Connection, dial DialFunc) (Session, error)
}

type Capabilities struct {
	Schemas        bool // PostgreSQL
	Indexes        bool // Index tab exists
	Triggers       bool // Triggers popover exists
	ForeignKeys    bool
	Views          bool
	MatViews       bool
	Functions      bool // functions + procedures
	Sequences      bool
	ReorderColumns bool // MySQL, ClickHouse
	RenameDB       bool
	Transactions   bool // Apply() is atomic
	ProcessList    bool
	Users          bool
	Document       bool     // grid also offers Json/Tree views (MongoDB, Redis)
	Editor         string   // "sql" | "redis" | "mongo"
	Dialect        string   // "postgres" | "mysql" | "clickhouse" | "redis" | "mongo" | "sqlite" | "duckdb" | "cql" | "partiql"
	DatabaseNoun   string   // "database" (SQL, Mongo) or "db index" (Redis)
	Types          []string // column types offered by the structure editor / Set Value; empty = the PostgreSQL list
	Explain        string   // plan format: "" = by Dialect, "text" = plain EXPLAIN, "none" = unsupported
}

// ExplainMode is the key ExplainStatement and ExplainTree take: the Dialect unless the profile says otherwise.
func (c Capabilities) ExplainMode() string {
	if c.Explain != "" {
		return c.Explain
	}
	return c.Dialect
}

type Kind string

const (
	KindTable     Kind = "table" // also Mongo collections and Redis keys
	KindView      Kind = "view"
	KindMatView   Kind = "matview"
	KindFunction  Kind = "function"
	KindProcedure Kind = "procedure"
	KindSequence  Kind = "sequence"
	KindType      Kind = "type"
)

// ObjRef names an object. Schema is "" for engines without schemas.
type ObjRef struct {
	Schema string
	Name   string
	Kind   Kind
}

type Object struct {
	ObjRef
	Detail string // row estimate / size / redis key type, shown dimmed
}

type Column struct {
	Name     string
	OrigName string // set by the UI when loaded from the server; Name != OrigName means rename, "" means new column
	Type     string
	Nullable bool
	Default  string // SQL expression text; "" = none
	HasDef   bool
	PK       bool
	Extra    string // auto_increment, identity, comment...
	FK       *ForeignKey
}

type ForeignKey struct {
	Name       string
	Columns    []string
	RefSchema  string
	RefTable   string
	RefColumns []string
	OnUpdate   string // NO ACTION | RESTRICT | CASCADE | SET NULL | SET DEFAULT
	OnDelete   string
}

type Index struct {
	Name      string
	Columns   []string
	Unique    bool
	Primary   bool
	Algorithm string
	Condition string
}

type Trigger struct {
	Name      string
	Timing    string // BEFORE | AFTER | INSTEAD OF
	Event     string // INSERT | UPDATE | DELETE...
	Statement string
}

// TableDef is the editable structure of a table.
type TableDef struct {
	Schema      string
	Name        string
	Columns     []Column
	PrimaryKey  []string
	Indexes     []Index
	ForeignKeys []ForeignKey
}

type ResultColumn struct {
	Name string
	Type string
}

// Result is one statement's outcome. Rows hold normalized values: nil, string, []byte,
// bool, int64, uint64, float64, time.Time, or json-ish text for nested documents.
type Result struct {
	Columns   []ResultColumn
	Rows      [][]any
	Docs      []string // optional raw JSON per row for Json/Tree views (Document engines)
	Affected  int64    // for statements without a row set
	HasRows   bool     // false for INSERT/UPDATE/DDL: show Message view
	Message   string   // server notices, "3 rows affected"
	Duration  time.Duration
	Truncated bool // stopped at the row limit
}

type Sort struct {
	Column string
	Desc   bool
}

type RowQuery struct {
	ObjRef
	Filters FilterSet
	Sort    []Sort
	Columns []string // visible columns; empty = all
	Limit   int
	Offset  int
}

type Count struct {
	N     int64
	Exact bool
}

// Special cell values the UI hands to Plan.
type special int

const (
	Null    special = iota // SQL NULL
	Default                // column DEFAULT
	Empty                  // empty string
)

// Expr is a raw SQL expression such as NOW(); engines translate where needed.
type Expr string

type ChangeKind int

const (
	Insert ChangeKind = iota
	Update
	Delete
)

type KeyVal struct {
	Col string
	Val any // string | nil/Null/Default/Empty | Expr | []byte | other driver values
}

// Change is one pending GUI edit. Key identifies the row (PK columns with original values,
// or every original value when the table has no PK).
type Change struct {
	Kind ChangeKind
	ObjRef
	Key []KeyVal
	Set []KeyVal
}

// Statement is something Apply can run. Text is shown in Preview and the console log.
// SQL engines run Text with Args; Redis and Mongo parse Text themselves and leave Args nil.
type Statement struct {
	Text     string
	Args     []any
	Display  string // Text with args inlined, for Preview/history; falls back to Text
	ReadOnly bool
}

func (s Statement) Shown() string {
	if s.Display != "" {
		return s.Display
	}
	return s.Text
}

type AdminOp string

const (
	OpDropObject     AdminOp = "drop"            // opts: cascade
	OpTruncate       AdminOp = "truncate"        // opts: cascade, restart_identity
	OpClone          AdminOp = "clone"           // opts: new_name, with_data
	OpRenameObject   AdminOp = "rename"          // opts: new_name
	OpCreateDatabase AdminOp = "create_database" // ref.Name; opts: encoding, collation
	OpDropDatabase   AdminOp = "drop_database"
	OpRenameDatabase AdminOp = "rename_database" // opts: new_name
	OpCreateSchema   AdminOp = "create_schema"   // ref.Schema (or ref.Name); PostgreSQL. MySQL / ClickHouse: schema = database, use OpCreateDatabase
	OpDropSchema     AdminOp = "drop_schema"     // opts: cascade
)

type ScriptKind string

const (
	ScriptCreate    ScriptKind = "create"
	ScriptDrop      ScriptKind = "drop"
	ScriptTruncate  ScriptKind = "truncate"
	ScriptSelectTop ScriptKind = "select100"
)

// Session is one open connection. All methods are safe to call from different goroutines
// than the one that opened it; Exec/Rows must honor ctx cancellation (that is how Cancel works).
type Session interface {
	Caps() Capabilities
	ServerInfo() string // e.g. "PostgreSQL 16.2"
	Ping(ctx context.Context) error
	Close() error

	Databases(ctx context.Context) ([]string, error)
	CurrentDatabase() string
	UseDatabase(ctx context.Context, name string) error

	Schemas(ctx context.Context) ([]string, error) // nil when !Caps.Schemas
	CurrentSchema() string
	Objects(ctx context.Context, schema string) ([]Object, error)
	Columns(ctx context.Context, ref ObjRef) ([]Column, error)
	Indexes(ctx context.Context, ref ObjRef) ([]Index, error)
	Triggers(ctx context.Context, ref ObjRef) ([]Trigger, error)
	ForeignKeys(ctx context.Context, ref ObjRef) ([]ForeignKey, error)
	DDL(ctx context.Context, ref ObjRef) (string, error)
	Script(ctx context.Context, kind ScriptKind, ref ObjRef) (string, error) // "Copy Script As"

	Rows(ctx context.Context, q RowQuery) (*Result, error)
	Count(ctx context.Context, q RowQuery, estimate bool) (Count, error)
	// Exec runs ONE statement (the UI splits with Split). limit<=0 means no limit.
	Exec(ctx context.Context, stmt string, limit int) (*Result, error)

	Plan(changes []Change) ([]Statement, error)
	PlanAdmin(op AdminOp, ref ObjRef, opts map[string]string) ([]Statement, error)
	// PlanTable diffs old->new (old nil = create). Errors for engines without structure editing.
	PlanTable(old, new *TableDef) ([]Statement, error)
	// Apply runs the statements, in one transaction when Caps.Transactions.
	Apply(ctx context.Context, stmts []Statement) error
}

// Optional interfaces (M2/M3), found by type assertion.
type ProcessInfo struct {
	ID      string
	User    string
	DB      string
	State   string
	Elapsed time.Duration
	Query   string
}

type ProcessLister interface {
	Processes(ctx context.Context) ([]ProcessInfo, error)
	Kill(ctx context.Context, id string) error
}

// User is a database role (PostgreSQL) or account (MySQL).
type User struct {
	Name   string
	Host   string         // MySQL only ("%", "localhost"); "" for PostgreSQL
	Attrs  []string       // PG: SUPERUSER, CREATEDB, CREATEROLE, LOGIN, REPLICATION...; MySQL: global privileges (SELECT, INSERT, ..., ALL)
	Grants []Grant        // table/database privileges
	Limits map[string]int // MySQL: MAX_QUERIES_PER_HOUR, MAX_UPDATES_PER_HOUR, MAX_CONNECTIONS_PER_HOUR, MAX_USER_CONNECTIONS; PG: CONNECTION_LIMIT
}

// Grant is a set of privileges on one object: "schema.table" (PG) or "db.*" / "db.table" (MySQL).
type Grant struct {
	Object  string
	Privs   []string
	Columns []string // column-level privilege (SELECT, INSERT, UPDATE, REFERENCES) on just these columns; empty = whole object
}

// UserChange is one administrative edit. Kind: create | drop | password | attrs | grant | revoke.
// attrs on PostgreSQL is ALTER ROLE ... WITH <Attrs> (NOSUPERUSER etc. turn a flag off); on MySQL it
// grants User.Attrs globally (GRANT ... ON *.*).
type UserChange struct {
	Kind     string
	User     User
	Password string
	Grant    Grant
}

// UserAdmin is implemented by sessions that can list and edit users; PlanUser returns statements for
// Apply, so safe mode and Preview see them. Statement.Display hides the password.
type UserAdmin interface {
	Users(ctx context.Context) ([]User, error)
	PlanUser(c UserChange) ([]Statement, error)
}
