package db

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type SearchOptions struct {
	Value  string
	Op     string // "Contains" | "Not contains" | "=" | "Has prefix" | "Has suffix"
	Schema string // "" = current
	// AllSchemas walks every schema from Session.Schemas (Schema is ignored); each hit's Ref.Schema says where it is.
	// shortcut: system schemas are skipped by name (pg_catalog, information_schema); a session without schemas searches the current one.
	AllSchemas bool
	Tables     []string // table names to search; empty = every KindTable object in the schema
}

type SearchHit struct {
	Ref     ObjRef
	Count   int64
	Filters FilterSet // reproduces the hit, so the UI can open the table with it
	Err     error     // a table that failed (e.g. unsupported cast) is reported, not fatal
}

// Search counts matching rows table by table, calling hit after each one (zero counts included, for progress).
// Cancelling ctx stops it and returns ctx.Err(); the caller resumes by re-running with the remaining Tables.
func Search(ctx context.Context, sess Session, o SearchOptions, hit func(SearchHit)) error {
	if !slices.Contains(Operators(sess.Caps().Dialect), o.Op) {
		return fmt.Errorf("search operator %q is not supported on this connection", o.Op)
	}
	schemas, err := SearchSchemas(ctx, sess, o)
	if err != nil {
		return err
	}
	op, val := o.Op, o.Value // "=" stays "=": the filter builder casts to text on LikeCast dialects and compares natively elsewhere
	only := map[string]bool{}
	for _, t := range o.Tables {
		only[t] = true
	}
	for _, schema := range schemas {
		if schema == "" {
			schema = sess.CurrentSchema()
		}
		objs, err := sess.Objects(ctx, schema)
		if err != nil {
			return err
		}
		if err := searchSchema(ctx, sess, objs, only, op, val, hit); err != nil {
			return err
		}
	}
	return nil
}

func searchSchema(ctx context.Context, sess Session, objs []Object, only map[string]bool, op, val string, hit func(SearchHit)) error {
	for _, ob := range objs {
		if ob.Kind != KindTable || len(only) > 0 && !only[ob.Name] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		h := SearchHit{Ref: ob.ObjRef, Filters: FilterSet{Filters: []Filter{{Column: AnyColumn, Op: op, Value: val, Enabled: true}}}}
		n, err := sess.Count(ctx, RowQuery{ObjRef: ob.ObjRef, Filters: h.Filters}, false)
		h.Count = n.N
		if cerr := ctx.Err(); cerr != nil { // cancelled mid-count: the error is the cancel, not the table's
			return cerr
		}
		h.Err = err
		hit(h)
	}
	return nil
}

// SearchSchemas is the schemas Search walks: just o.Schema, or with AllSchemas every non-system one.
func SearchSchemas(ctx context.Context, sess Session, o SearchOptions) ([]string, error) {
	if !o.AllSchemas {
		return []string{o.Schema}, nil
	}
	var all []string
	var err error
	if d := sess.Caps().Dialect; !sess.Caps().Schemas && d != "mysql" && d != "mariadb" && d != "clickhouse" {
		// Objects ignores its schema argument here (Redis, Mongo, ...): walking every "database" would scan the current one N times.
		return []string{""}, nil
	}
	if sess.Caps().Schemas {
		all, err = sess.Schemas(ctx)
	} else if all, err = sess.Databases(ctx); err == nil && len(all) == 0 {
		return nil, fmt.Errorf("searching all schemas is not supported on this connection")
	}
	if err != nil {
		return nil, err
	}
	var schemas []string
	for _, sc := range all {
		if !systemSchema(sess.Caps().Dialect, sc) {
			schemas = append(schemas, sc)
		}
	}
	if len(schemas) == 0 {
		schemas = []string{""}
	}
	return schemas, nil
}

// systemSchema reports whether sc is an engine-internal schema/database that "all schemas" search skips.
func systemSchema(dialect, sc string) bool {
	switch dialect {
	case "mysql", "mariadb":
		switch strings.ToLower(sc) {
		case "mysql", "sys", "performance_schema", "information_schema":
			return true
		}
		return false
	case "clickhouse":
		return sc == "system" || strings.EqualFold(sc, "information_schema")
	}
	return sc == "information_schema" || sc == "pg_catalog" || strings.HasPrefix(sc, "pg_toast") || strings.HasPrefix(sc, "pg_temp")
}
