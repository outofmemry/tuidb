package cassandra

import (
	"context"
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestIntegrationCatalog(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	objs, err := s.Objects(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]db.Kind{}
	for _, o := range objs {
		kinds[o.Name] = o.Kind
	}
	for name, k := range map[string]db.Kind{"customers": db.KindTable, "orders": db.KindTable, "events": db.KindTable, "hits": db.KindTable,
		"orders_by_status": db.KindMatView, "address": db.KindType, "add_one(int)": db.KindFunction, "total_sum(int)": db.KindFunction} {
		if kinds[name] != k {
			t.Errorf("object %s: kind %q want %q (have %v)", name, kinds[name], k, kinds)
		}
	}

	cols, err := s.Columns(ctx, ref("orders"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cols {
		got = append(got, c.Name+" "+c.Type+" "+c.Extra)
	}
	eq(t, "orders columns", got, []string{"customer_id int partition key", "id int clustering desc", "placed date ", "status text ", "total decimal "})
	if !cols[0].PK || !cols[1].PK || cols[2].PK || cols[2].Nullable != true || cols[0].Nullable || cols[2].OrigName != "placed" {
		t.Errorf("orders flags %+v", cols)
	}
	// the column list must come in the order SELECT * returns them: the grid pairs the two
	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("orders"), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var rc []string
	for _, c := range r.Columns {
		rc = append(rc, c.Name)
	}
	eq(t, "SELECT * order", rc, []string{"customer_id", "id", "placed", "status", "total"})
	ev, _ := s.Columns(ctx, ref("events"))
	got = nil
	for _, c := range ev {
		got = append(got, c.Name+" "+c.Extra)
	}
	eq(t, "events columns", got, []string{"tenant partition key", "ts clustering asc", "tenant_name static", "kind "})
	er, _ := s.Rows(ctx, db.RowQuery{ObjRef: ref("events"), Limit: 10})
	rc = nil
	for _, c := range er.Columns {
		rc = append(rc, c.Name)
	}
	eq(t, "events SELECT * order", rc, []string{"tenant", "ts", "tenant_name", "kind"})
	if _, err := s.Columns(ctx, ref("nope")); err == nil {
		t.Error("missing table must error")
	}
	cc, _ := s.Columns(ctx, db.ObjRef{Name: "orders_by_status", Kind: db.KindMatView})
	if len(cc) != 5 || cc[0].Name != "status" {
		t.Errorf("matview columns %+v", cc)
	}

	idx, err := s.Indexes(ctx, ref("orders"))
	if err != nil || len(idx) != 1 || idx[0].Name != "orders_status_idx" || idx[0].Columns[0] != "status" || idx[0].Algorithm != "" {
		t.Errorf("orders indexes %+v %v", idx, err)
	}
	if fks, err := s.ForeignKeys(ctx, ref("orders")); err != nil || len(fks) != 0 {
		t.Errorf("fks %v %v", fks, err)
	}
	if trg, err := s.Triggers(ctx, ref("orders")); err != nil || len(trg) != 0 {
		t.Errorf("triggers %v %v", trg, err)
	}

	for _, o := range []db.ObjRef{ref("customers"), {Name: "orders_by_status", Kind: db.KindMatView}, {Name: "address", Kind: db.KindType},
		{Name: "add_one(int)", Kind: db.KindFunction}, {Name: "total_sum(int)", Kind: db.KindFunction}} {
		d, err := s.DDL(ctx, o)
		if err != nil || !strings.HasPrefix(d, "CREATE") || !strings.HasSuffix(d, ";") {
			t.Errorf("ddl %s: %q %v", o.Name, d, err)
		}
	}
	d, _ := s.DDL(ctx, ref("orders"))
	if !strings.Contains(d, "CLUSTERING ORDER BY (id DESC)") || !strings.Contains(d, "webtuidb.orders") {
		t.Errorf("orders ddl %q", d)
	}
	d, _ = s.DDL(ctx, db.ObjRef{Name: "add_one(int)", Kind: db.KindFunction})
	if !strings.Contains(d, "CREATE OR REPLACE FUNCTION") || !strings.Contains(d, "$$ return x == null ? null : x + 1; $$") {
		t.Errorf("function ddl %q", d)
	}
	// the generated function and aggregate DDL must run on the server
	for _, o := range []db.ObjRef{{Name: "add_one(int)", Kind: db.KindFunction}, {Name: "total_sum(int)", Kind: db.KindFunction}, {Name: "address", Kind: db.KindType}} {
		d, _ := s.DDL(ctx, o)
		if o.Kind == db.KindType {
			d = strings.Replace(d, "CREATE TYPE", "CREATE TYPE IF NOT EXISTS", 1)
		}
		exec(t, s, d)
	}

	for kind, want := range map[db.ScriptKind]string{db.ScriptDrop: `DROP TABLE "webtuidb"."orders";`, db.ScriptTruncate: `TRUNCATE "webtuidb"."orders";`,
		db.ScriptSelectTop: `SELECT * FROM "webtuidb"."orders" LIMIT 100;`} {
		if got, err := s.Script(ctx, kind, ref("orders")); err != nil || got != want {
			t.Errorf("script %s: %q %v", kind, got, err)
		}
	}
	if got, _ := s.Script(ctx, db.ScriptDrop, db.ObjRef{Name: "total_sum(int)", Kind: db.KindFunction}); got != `DROP AGGREGATE "webtuidb"."total_sum"(int);` {
		t.Errorf("drop aggregate %q", got)
	}
	if got, _ := s.Script(ctx, db.ScriptDrop, db.ObjRef{Name: "add_one(int)", Kind: db.KindFunction}); got != `DROP FUNCTION "webtuidb"."add_one"(int);` {
		t.Errorf("drop function %q", got)
	}
}

func TestIntegrationObjectsDetail(t *testing.T) {
	s := open(t)
	// size_estimates is empty until Cassandra computes it: only the shape is checked
	objs, err := s.Objects(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.Detail != "" && o.Detail != "aggregate" && !strings.HasPrefix(o.Detail, "~") {
			t.Errorf("detail %q", o.Detail)
		}
	}
}
