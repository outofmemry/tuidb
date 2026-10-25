package cassandra

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"webtuidb/internal/db"
)

// maps runs a metadata query and returns its rows as column-name maps.
func (s *session) maps(ctx context.Context, text string, args ...any) ([]map[string]any, error) {
	it := s.query(text, args...).IterContext(ctx)
	rows, err := it.SliceMap()
	if err != nil {
		it.Close()
		return nil, err
	}
	return rows, it.Close()
}

func str(v any) string { s, _ := v.(string); return s }

func strs(v any) []string { l, _ := v.([]string); return l }

// Databases lists the keyspaces; the system ones come last.
func (s *session) Databases(ctx context.Context) ([]string, error) {
	rows, err := s.maps(ctx, "SELECT keyspace_name FROM system_schema.keyspaces")
	if err != nil {
		return nil, fmt.Errorf("list keyspaces: %w", err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, str(r["keyspace_name"]))
	}
	sort.Slice(out, func(i, j int) bool {
		si, sj := isSystem(out[i]), isSystem(out[j])
		if si != sj {
			return sj
		}
		return out[i] < out[j]
	})
	return out, nil
}

func isSystem(ks string) bool {
	return ks == "system" || strings.HasPrefix(ks, "system_") || ks == "dse_system" || strings.HasPrefix(ks, "dse_")
}

// signature renders "name(arg, arg)": functions are overloaded, so the argument types are part of the name the UI shows.
func signature(name string, args []string) string { return name + "(" + strings.Join(args, ", ") + ")" }

// splitSignature undoes signature.
func splitSignature(sig string) (name, args string) {
	if i := strings.IndexByte(sig, '('); i >= 0 && strings.HasSuffix(sig, ")") {
		return sig[:i], sig[i+1 : len(sig)-1]
	}
	return sig, ""
}

// Objects lists tables, materialized views, types, functions and aggregates. Detail is the estimated partition count.
func (s *session) Objects(ctx context.Context, _ string) ([]db.Object, error) {
	ks := s.CurrentDatabase()
	if ks == "" {
		return nil, errors.New("no keyspace selected")
	}
	est := map[string]int64{}
	if rows, err := s.maps(ctx, "SELECT table_name, partitions_count FROM system.size_estimates WHERE keyspace_name = ?", ks); err == nil {
		for _, r := range rows {
			n, _ := r["partitions_count"].(int64)
			est[str(r["table_name"])] += n
		}
	}
	var out []db.Object
	for _, l := range []struct {
		kind       db.Kind
		table, col string
	}{
		{db.KindTable, "tables", "table_name"}, {db.KindMatView, "views", "view_name"}, {db.KindType, "types", "type_name"},
	} {
		rows, err := s.maps(ctx, "SELECT "+l.col+" FROM system_schema."+l.table+" WHERE keyspace_name = ?", ks)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", l.table, err)
		}
		for _, r := range rows {
			o := db.Object{ObjRef: db.ObjRef{Name: str(r[l.col]), Kind: l.kind}}
			if n := est[o.Name]; n > 0 && l.kind != db.KindType {
				o.Detail = "~" + strconv.FormatInt(n, 10)
			}
			out = append(out, o)
		}
	}
	aggs := map[string]bool{}
	for _, l := range []struct{ table, col string }{{"functions", "function_name"}, {"aggregates", "aggregate_name"}} {
		rows, err := s.maps(ctx, "SELECT "+l.col+", argument_types FROM system_schema."+l.table+" WHERE keyspace_name = ?", ks)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", l.table, err)
		}
		for _, r := range rows {
			sig := signature(str(r[l.col]), strs(r["argument_types"]))
			aggs[sig] = l.table == "aggregates"
			o := db.Object{ObjRef: db.ObjRef{Name: sig, Kind: db.KindFunction}}
			if l.table == "aggregates" {
				o.Detail = "aggregate"
			}
			out = append(out, o)
		}
	}
	s.mu.Lock()
	s.aggs = aggs
	s.mu.Unlock()
	return out, nil
}

// kindRank is the order SELECT * returns columns in.
var kindRank = map[string]int{"partition_key": 0, "clustering": 1, "static": 2, "regular": 3}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return nil, err
	}
	rows, err := s.maps(ctx, "SELECT column_name, kind, position, type, clustering_order FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?", ks, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool {
		ki, kj := kindRank[str(rows[i]["kind"])], kindRank[str(rows[j]["kind"])]
		switch {
		case ki != kj:
			return ki < kj
		case ki < 2:
			pi, _ := rows[i]["position"].(int)
			pj, _ := rows[j]["position"].(int)
			return pi < pj
		}
		return str(rows[i]["column_name"]) < str(rows[j]["column_name"])
	})
	var out []db.Column
	for _, r := range rows {
		kind := str(r["kind"])
		c := db.Column{Name: str(r["column_name"]), Type: str(r["type"]), PK: kind == "partition_key" || kind == "clustering", Nullable: kind == "static" || kind == "regular"}
		c.OrigName = c.Name
		switch kind {
		case "partition_key":
			c.Extra = "partition key"
		case "clustering":
			c.Extra = "clustering " + str(r["clustering_order"])
		case "static":
			c.Extra = "static"
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("table %s.%s not found", ks, ref.Name)
	}
	return out, nil
}

func (s *session) Indexes(ctx context.Context, ref db.ObjRef) ([]db.Index, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return nil, err
	}
	rows, err := s.maps(ctx, "SELECT index_name, kind, options FROM system_schema.indexes WHERE keyspace_name = ? AND table_name = ?", ks, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	var out []db.Index
	for _, r := range rows {
		opts, _ := r["options"].(map[string]string)
		ix := db.Index{Name: str(r["index_name"]), Columns: []string{opts["target"]}}
		switch class := opts["class_name"]; {
		case strings.Contains(class, "StorageAttachedIndex"):
			ix.Algorithm = "SAI"
		case strings.Contains(class, "SASIIndex"):
			ix.Algorithm = "SASI"
		case class != "":
			ix.Algorithm = class
		}
		out = append(out, ix)
	}
	return out, nil
}

// Triggers lists a table's triggers. They are Java classes that must be on the server's classpath: the UI shows the class
// name, and there is no SQL body to edit.
func (s *session) Triggers(ctx context.Context, ref db.ObjRef) ([]db.Trigger, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return nil, err
	}
	rows, err := s.maps(ctx, "SELECT trigger_name, options FROM system_schema.triggers WHERE keyspace_name = ? AND table_name = ?", ks, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	var out []db.Trigger
	for _, r := range rows {
		opts, _ := r["options"].(map[string]string)
		out = append(out, db.Trigger{Name: str(r["trigger_name"]), Event: "Java class", Statement: opts["class"]})
	}
	return out, nil
}

func (s *session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }

// DDL returns the CREATE statement. Tables and materialized views use the server's DESCRIBE (Cassandra 4+), which keeps
// every table option; older servers get a statement rebuilt from system_schema (columns and keys only).
func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	ks, err := s.ksOf(ref)
	if err != nil {
		return "", err
	}
	switch ref.Kind {
	case db.KindTable, db.KindMatView:
		kw := "TABLE"
		if ref.Kind == db.KindMatView {
			kw = "MATERIALIZED VIEW"
		}
		rows, err := s.maps(ctx, "DESCRIBE "+kw+" "+q(ks)+"."+q(ref.Name))
		if err == nil && len(rows) > 0 {
			return strings.TrimSpace(str(rows[0]["create_statement"])), nil
		}
		var se *gocql.RequestErrSyntax // servers before 4.0 do not know DESCRIBE; any other failure is real
		if ref.Kind == db.KindTable && (err == nil || errors.As(err, &se)) {
			ddl, err := s.rebuiltDDL(ctx, ref)
			if err != nil {
				return "", err
			}
			return ddl + "\n-- Rebuilt from system_schema because DESCRIBE is unavailable: table options and indexes are missing.", nil
		}
		return "", fmt.Errorf("describe %s: %v", ref.Name, err)
	case db.KindType:
		rows, err := s.maps(ctx, "SELECT field_names, field_types FROM system_schema.types WHERE keyspace_name = ? AND type_name = ?", ks, ref.Name)
		if err != nil || len(rows) == 0 {
			return "", fmt.Errorf("type %s.%s not found: %v", ks, ref.Name, err)
		}
		names, types := strs(rows[0]["field_names"]), strs(rows[0]["field_types"])
		fields := make([]string, len(names))
		for i := range names {
			fields[i] = "    " + q(names[i]) + " " + types[i]
		}
		return "CREATE TYPE " + q(ks) + "." + q(ref.Name) + " (\n" + strings.Join(fields, ",\n") + "\n);", nil
	case db.KindFunction:
		return s.functionDDL(ctx, ks, ref.Name)
	}
	return "", fmt.Errorf("no DDL for a %s", ref.Kind)
}

func (s *session) rebuiltDDL(ctx context.Context, ref db.ObjRef) (string, error) {
	cols, err := s.Columns(ctx, ref)
	if err != nil {
		return "", err
	}
	ks, _ := s.ksOf(ref)
	stmts, err := createTable(&db.TableDef{Schema: ks, Name: ref.Name, Columns: cols})
	if err != nil {
		return "", err
	}
	return stmts[0].Text + ";", nil
}

func quoteLit(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

// body writes a function body as a $$ string unless it contains $$ itself.
func body(v string) string {
	if strings.Contains(v, "$$") {
		return quoteLit(v)
	}
	return "$$" + v + "$$"
}

// functionDDL rebuilds a function or aggregate from system_schema; sig is "name(arg types)".
func (s *session) functionDDL(ctx context.Context, ks, sig string) (string, error) {
	name, args := splitSignature(sig)
	var argTypes []string
	if args != "" {
		argTypes = splitTop(args)
	}
	for _, aggregate := range []bool{false, true} {
		table, col := "functions", "function_name"
		if aggregate {
			table, col = "aggregates", "aggregate_name"
		}
		rows, err := s.maps(ctx, "SELECT * FROM system_schema."+table+" WHERE keyspace_name = ? AND "+col+" = ?", ks, name)
		if err != nil {
			return "", err
		}
		for _, r := range rows {
			if strings.Join(strs(r["argument_types"]), ",") != strings.Join(argTypes, ",") {
				continue
			}
			if aggregate {
				t := "CREATE OR REPLACE AGGREGATE " + q(ks) + "." + q(name) + " (" + strings.Join(argTypes, ", ") + ")\n    SFUNC " + q(str(r["state_func"])) + "\n    STYPE " + str(r["state_type"])
				if f := str(r["final_func"]); f != "" {
					t += "\n    FINALFUNC " + q(f)
				}
				if ic := str(r["initcond"]); ic != "" {
					t += "\n    INITCOND " + ic
				}
				return t + ";", nil
			}
			names := strs(r["argument_names"])
			params := make([]string, len(names))
			for i := range names {
				params[i] = q(names[i]) + " " + argTypes[i]
			}
			null := "RETURNS NULL ON NULL INPUT"
			if b, _ := r["called_on_null_input"].(bool); b {
				null = "CALLED ON NULL INPUT"
			}
			return "CREATE OR REPLACE FUNCTION " + q(ks) + "." + q(name) + " (" + strings.Join(params, ", ") + ")\n    " + null +
				"\n    RETURNS " + str(r["return_type"]) + "\n    LANGUAGE " + str(r["language"]) + "\n    AS " + body(str(r["body"])) + ";", nil
		}
	}
	return "", fmt.Errorf("function %s.%s not found", ks, sig)
}

// splitTop splits "a, map<int, text>, b" at the commas outside <>.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '<', '(':
			depth++
		case '>', ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// dropKeyword is the DROP statement's object keyword.
func (s *session) dropTarget(ref db.ObjRef) (string, error) {
	qn, err := s.qual(ref)
	if err != nil {
		return "", err
	}
	switch ref.Kind {
	case db.KindTable:
		return "TABLE " + qn, nil
	case db.KindMatView:
		return "MATERIALIZED VIEW " + qn, nil
	case db.KindType:
		return "TYPE " + qn, nil
	case db.KindFunction:
		ks, _ := s.ksOf(ref)
		name, args := splitSignature(ref.Name)
		s.mu.RLock()
		agg := s.aggs[ref.Name]
		s.mu.RUnlock()
		kw := "FUNCTION "
		if agg {
			kw = "AGGREGATE "
		}
		t := kw + q(ks) + "." + q(name)
		if strings.HasSuffix(ref.Name, ")") {
			t += "(" + args + ")"
		}
		return t, nil
	}
	return "", fmt.Errorf("cannot drop a %s", ref.Kind)
}

func (s *session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	switch kind {
	case db.ScriptCreate:
		return s.DDL(ctx, ref)
	case db.ScriptDrop:
		t, err := s.dropTarget(ref)
		return "DROP " + t + ";", err
	case db.ScriptTruncate:
		if ref.Kind != db.KindTable {
			return "", fmt.Errorf("only tables can be truncated, not a %s", ref.Kind)
		}
		qn, err := s.qual(ref)
		return "TRUNCATE " + qn + ";", err
	case db.ScriptSelectTop:
		if ref.Kind != db.KindTable && ref.Kind != db.KindMatView {
			return "", fmt.Errorf("a %s has no rows to select", ref.Kind)
		}
		qn, err := s.qual(ref)
		return "SELECT * FROM " + qn + " LIMIT 100;", err
	}
	return "", fmt.Errorf("unknown script kind %q", kind)
}
