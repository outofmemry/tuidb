package tools

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/db"
)

func TestSchemaDiff(t *testing.T) {
	a, b, c := db.TableDef{Name: "a"}, db.TableDef{Name: "b"}, db.TableDef{Schema: "s", Name: "c"}
	plan := func(o, n *db.TableDef) ([]db.Statement, error) {
		kind := "alter"
		if o == nil {
			kind = "create"
		}
		return []db.Statement{{Text: kind + " " + n.Name}}, nil
	}
	got, err := SchemaDiff([]db.TableDef{a, c}, []db.TableDef{b, a}, plan)
	var txt []string
	for _, s := range got {
		txt = append(txt, s.Text)
	}
	if want := []string{"create b", "alter a", `DROP TABLE "s"."c"`}; err != nil || !reflect.DeepEqual(txt, want) {
		t.Errorf("%v %v", txt, err)
	}
	got, _ = SchemaDiff([]db.TableDef{a}, nil, plan, db.MySQL.Quote)
	if got[0].Text != "DROP TABLE `a`" {
		t.Error(got[0].Text)
	}
}

func res(cols []string, rows ...[]any) *db.Result {
	r := &db.Result{Rows: rows}
	for _, c := range cols {
		r.Columns = append(r.Columns, db.ResultColumn{Name: c})
	}
	return r
}

func TestDataDiff(t *testing.T) {
	cols := []string{"id", "n"}
	from := res(cols, []any{int64(1), "a"}, []any{int64(2), "b"}, []any{int64(3), nil})
	to := res(cols, []any{int64(1), "a"}, []any{int64(2), "B"}, []any{int64(4), "d"})
	got := DataDiff(from, to, []string{"id"})
	want := []db.Change{
		{Kind: db.Update, Key: []db.KeyVal{{Col: "id", Val: int64(2)}}, Set: []db.KeyVal{{Col: "n", Val: "B"}}},
		{Kind: db.Insert, Set: []db.KeyVal{{Col: "id", Val: int64(4)}, {Col: "n", Val: "d"}}},
		{Kind: db.Delete, Key: []db.KeyVal{{Col: "id", Val: int64(3)}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	// without a pk every column is the key: a changed row is an insert plus a delete
	if got := DataDiff(from, to, nil); len(got) != 4 {
		t.Errorf("no pk: %+v", got)
	}
}

func TestDataDiffCheckedRefusesDuplicates(t *testing.T) {
	cols := []string{"n"}
	from := res(cols, []any{"a"}, []any{"a"})
	to := res(cols, []any{"a"})
	if _, err := DataDiffChecked(from, to, nil); err == nil {
		t.Error("identical keyless rows must be refused")
	}
	if _, err := DataDiffChecked(to, to, nil); err != nil {
		t.Error(err)
	}
}

func TestBulkUpdate(t *testing.T) {
	fs := db.FilterSet{Filters: []db.Filter{{Column: "a", Op: "=", Value: "x", Enabled: true}, {Column: "b", Op: "IS NULL", Enabled: true}}}
	set := []db.KeyVal{{Col: "c", Val: "it's"}, {Col: "d", Val: db.Expr("NOW()")}}
	st, err := BulkUpdate(db.ObjRef{Schema: "public", Name: "t"}, fs, set, "postgres")
	if err != nil || st.Text != `UPDATE "public"."t" SET "c" = $1, "d" = NOW() WHERE "a" = $2 AND "b" IS NULL` ||
		!reflect.DeepEqual(st.Args, []any{"it's", "x"}) || st.Display != `UPDATE "public"."t" SET "c" = 'it''s', "d" = NOW() WHERE "a" = 'x' AND "b" IS NULL` {
		t.Errorf("%+v %v", st, err)
	}
	if st, _ := BulkUpdate(db.ObjRef{Name: "t"}, fs, set[:1], "clickhouse"); !strings.HasPrefix(st.Text, "ALTER TABLE `t` UPDATE `c` = ? WHERE") {
		t.Error(st.Text)
	}
	for _, bad := range []func() error{
		func() error { _, e := BulkUpdate(db.ObjRef{Name: "t"}, db.FilterSet{}, set, "mysql"); return e },
		func() error { _, e := BulkUpdate(db.ObjRef{Name: "t"}, fs, nil, "mysql"); return e },
		func() error { _, e := BulkUpdate(db.ObjRef{Name: "t"}, fs, set, "redis"); return e },
	} {
		if bad() == nil {
			t.Error("expected an error")
		}
	}
}

func TestFakeRows(t *testing.T) {
	cols := []db.Column{
		{Name: "id", Type: "integer", PK: true, HasDef: true}, {Name: "k", Type: "int", PK: true},
		{Name: "name", Type: "text"}, {Name: "ok", Type: "boolean"}, {Name: "price", Type: "numeric(8,2)"},
		{Name: "at", Type: "timestamp with time zone"}, {Name: "j", Type: "jsonb"}, {Name: "n", Type: "bigint"},
	}
	rows := FakeRows(cols, 3, 7)
	if len(rows) != 3 || FakeRows(cols, 3, 7)[2].Set[1].Val != rows[2].Set[1].Val {
		t.Fatalf("%+v", rows)
	}
	set := rows[1].Set
	if len(set) != 7 || set[0].Col != "k" || set[0].Val != pkBase(intMax("int"), 3, 7)+2 {
		t.Errorf("pk with default must be skipped, others counted: %+v", set)
	}
	if _, ok := set[2].Val.(bool); !ok {
		t.Error("bool")
	}
	if _, ok := set[3].Val.(float64); !ok {
		t.Error("float")
	}
	if at, ok := set[4].Val.(time.Time); !ok || time.Since(at) < 0 || time.Since(at) > 31*24*time.Hour {
		t.Errorf("time %v", set[4].Val)
	}
	if set[5].Val != "{}" {
		t.Error("json")
	}
}

func TestFakeRowsRespectIntegerRange(t *testing.T) {
	cols := []db.Column{{Name: "id", Type: "tinyint", PK: true}, {Name: "v", Type: "tinyint"}, {Name: "p", Type: "point"}}
	for _, r := range FakeRows(cols, 100, 3) {
		for _, kv := range r.Set[:2] {
			if n, ok := kv.Val.(int64); !ok || n < 0 || n > 127 {
				t.Fatalf("%s = %v is out of range for tinyint", kv.Col, kv.Val)
			}
		}
		if _, ok := r.Set[2].Val.(string); !ok {
			t.Fatalf("a point column is not an integer: %v", r.Set[2].Val)
		}
	}
	a, b := FakeRows([]db.Column{{Name: "id", Type: "int", PK: true}}, 5, 1), FakeRows([]db.Column{{Name: "id", Type: "int", PK: true}}, 5, 2)
	if a[0].Set[0].Val == b[0].Set[0].Val || a[0].Set[0].Val == int64(1) {
		t.Errorf("PKs should start away from 1 and differ by seed: %v %v", a[0].Set[0].Val, b[0].Set[0].Val)
	}
}

// T-P2-13: FROM public, TO shop: a table created in public must reference public's tables, not shop's.
func TestRebaseForeignKeys(t *testing.T) {
	fks := []db.ForeignKey{{RefSchema: "shop", RefTable: "orders"}, {RefSchema: "common", RefTable: "currencies"}}
	to := []db.TableDef{{Schema: "shop", Name: "order_items", ForeignKeys: fks}}
	Rebase(to, "public")
	got := to[0].ForeignKeys
	if to[0].Schema != "public" || got[0].RefSchema != "public" || got[1].RefSchema != "common" {
		t.Errorf("rebased %+v", to[0])
	}
	if fks[0].RefSchema != "shop" {
		t.Error("the caller's foreign keys must not change")
	}
}

func TestSchemaDiffDependencyOrder(t *testing.T) {
	parent := db.TableDef{Name: "parent"}
	child := db.TableDef{Name: "child", ForeignKeys: []db.ForeignKey{{RefTable: "parent"}}}
	plan := func(o, n *db.TableDef) ([]db.Statement, error) {
		return []db.Statement{{Text: "create " + n.Name}}, nil
	}
	got, err := SchemaDiff(nil, []db.TableDef{child, parent}, plan)
	if err != nil || got[0].Text != "create parent" || got[1].Text != "create child" {
		t.Errorf("creates must follow FK order: %v %v", got, err)
	}
	got, _ = SchemaDiff([]db.TableDef{parent, child}, nil, plan)
	if got[0].Text != `DROP TABLE "child"` || got[1].Text != `DROP TABLE "parent"` {
		t.Errorf("drops must run children first: %v", got)
	}
	cyc1 := db.TableDef{Name: "a", ForeignKeys: []db.ForeignKey{{RefTable: "b"}}}
	cyc2 := db.TableDef{Name: "b", ForeignKeys: []db.ForeignKey{{RefTable: "a"}}}
	got, err = SchemaDiff(nil, []db.TableDef{cyc2, cyc1}, plan)
	if err != nil || len(got) != 2 {
		t.Errorf("a cycle must still produce both: %v %v", got, err)
	}
	keep, drops, _ := SchemaDiffParts([]db.TableDef{parent}, nil, plan)
	if len(keep) != 0 || len(drops) != 1 {
		t.Errorf("parts: %v %v", keep, drops)
	}
}
