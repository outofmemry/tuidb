package cassandra

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuidb/internal/db"
)

func f(col, op, val string) db.Filter {
	return db.Filter{Column: col, Op: op, Value: val, Enabled: true}
}

func fs(fl ...db.Filter) db.FilterSet { return db.FilterSet{Filters: fl} }

func TestIntegrationRows(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	rows := func(rq db.RowQuery) *db.Result {
		t.Helper()
		rq.Name = "orders"
		r, err := s.Rows(ctx, rq)
		if err != nil {
			t.Fatalf("%+v: %v", rq, err)
		}
		return r
	}
	// partition key: no ALLOW FILTERING needed, clustering order DESC by default
	r := rows(db.RowQuery{Filters: fs(f("customer_id", "=", "1")), Columns: []string{"id", "status"}})
	eq(t, "partition", cells(r), []string{"2|new", "1|paid"})
	if r.Message != "" || len(r.Columns) != 2 || r.Columns[0].Type != "int" || r.Columns[1].Type != "text" {
		t.Errorf("message %q columns %+v", r.Message, r.Columns)
	}
	r = rows(db.RowQuery{Filters: fs(f("customer_id", "=", "1")), Columns: []string{"id"}, Sort: []db.Sort{{Column: "id"}}})
	eq(t, "clustering sort", cells(r), []string{"1", "2"})
	r = rows(db.RowQuery{Filters: fs(f("customer_id", "IN", "1, 2")), Columns: []string{"customer_id", "id"}, Limit: 10})
	if len(r.Rows) != 3 {
		t.Errorf("IN: %v", cells(r))
	}

	// paging emulated with fetch+skip
	all := cells(rows(db.RowQuery{Columns: []string{"id"}}))
	if len(all) != 3 {
		t.Fatalf("all rows %v", all)
	}
	eq(t, "limit 2", cells(rows(db.RowQuery{Columns: []string{"id"}, Limit: 2})), all[:2])
	eq(t, "limit 2 offset 1", cells(rows(db.RowQuery{Columns: []string{"id"}, Limit: 2, Offset: 1})), all[1:])
	eq(t, "limit 1 offset 2", cells(rows(db.RowQuery{Columns: []string{"id"}, Limit: 1, Offset: 2})), all[2:])
	eq(t, "past the end", cells(rows(db.RowQuery{Columns: []string{"id"}, Limit: 5, Offset: 3})), nil)

	// a secondary index covers the filter; a plain column needs ALLOW FILTERING and says so
	r = rows(db.RowQuery{Filters: fs(f("status", "=", "paid")), Columns: []string{"id"}})
	if r.Message != "" || len(r.Rows) != 2 {
		t.Errorf("indexed filter: %q %v", r.Message, cells(r))
	}
	r = rows(db.RowQuery{Filters: fs(f("total", ">", "20")), Columns: []string{"id"}}) // decimal compared with text from the filter bar
	if !strings.Contains(r.Message, "ALLOW FILTERING") || len(r.Rows) != 2 {
		t.Errorf("filtering filter: %q %v", r.Message, cells(r))
	}
	r = rows(db.RowQuery{Filters: fs(f("placed", ">=", "2024-02-02")), Columns: []string{"id"}}) // date
	if len(r.Rows) != 2 {
		t.Errorf("date filter: %v", cells(r))
	}
	r = rows(db.RowQuery{Filters: fs(f(db.RawColumn, "", "customer_id = 2 AND id = 3")), Columns: []string{"id"}})
	eq(t, "raw filter", cells(r), []string{"3"})

	// what CQL cannot do fails with a clear message before reaching the server
	for _, bad := range []db.FilterSet{
		{Filters: []db.Filter{f("id", "=", "1"), f("status", "=", "new")}, Or: true},
		fs(f(db.AnyColumn, "=", "1")),
	} {
		if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("orders"), Filters: bad}); err == nil {
			t.Errorf("filter %+v must fail", bad)
		}
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("orders"), Sort: []db.Sort{{Column: "status"}}}); err == nil || !strings.Contains(err.Error(), "ORDER BY") {
		t.Errorf("sort on a regular column: %v", err)
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("orders"), Filters: fs(f("status", "LIKE", "pa%"))}); err == nil {
		t.Error("LIKE without an SAI index must fail")
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("orders"), Filters: fs(f("id", "=", "abc"))}); err == nil {
		t.Error("a non-numeric value for an int column must fail")
	}
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("nope")}); err == nil {
		t.Error("missing table")
	}
	if len(db.Operators("cql")) != 7 {
		t.Errorf("operators %v", db.Operators("cql"))
	}
}

func TestIntegrationSAIFilters(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (id int PRIMARY KEY, name text)")
	exec(t, s, "CREATE CUSTOM INDEX ON "+q(tbl)+" (name) USING 'StorageAttachedIndex' WITH OPTIONS = {'case_sensitive': 'false'}")
	exec(t, s, "INSERT INTO "+q(tbl)+" (id, name) VALUES (1, 'alpha')")
	exec(t, s, "INSERT INTO "+q(tbl)+" (id, name) VALUES (2, 'beta')")
	idx, err := s.Indexes(ctx, ref(tbl))
	if err != nil || len(idx) != 1 || idx[0].Algorithm != "SAI" {
		t.Fatalf("indexes %+v %v", idx, err)
	}
	r, err := s.Rows(ctx, db.RowQuery{ObjRef: ref(tbl), Filters: fs(f("name", "=", "ALPHA"))}) // the index is case-insensitive, and needs no ALLOW FILTERING
	if err != nil || len(r.Rows) != 1 || r.Message != "" {
		t.Errorf("SAI equality: %v %+v", err, r)
	}
	// LIKE is for SASI indexes (off by default); SAI in 5.0 rejects it and the server's message comes through
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref(tbl), Filters: fs(f("name", "LIKE", "al%"))}); err == nil || !strings.Contains(err.Error(), "LIKE") {
		t.Errorf("SAI LIKE: %v", err)
	}
	if c, err := s.Count(ctx, db.RowQuery{ObjRef: ref(tbl), Filters: fs(f("name", "=", "BETA"))}, false); err != nil || c.N != 1 {
		t.Errorf("SAI count %+v %v", c, err)
	}
	// a statement that ends in a line comment still parses: the driver adds the newline the server's lexer needs
	if r := exec(t, s, "SELECT count(*) FROM "+q(tbl)+" -- the end"); r.Rows[0][0] != int64(2) {
		t.Errorf("trailing comment: %v", r.Rows)
	}
}

func TestIntegrationCount(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	c, err := s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, false)
	if err != nil || c.N != 3 || !c.Exact {
		t.Errorf("exact %+v %v", c, err)
	}
	c, err = s.Count(ctx, db.RowQuery{ObjRef: ref("orders"), Filters: fs(f("total", ">", "20"))}, true) // filtered: never an estimate
	if err != nil || c.N != 2 || !c.Exact {
		t.Errorf("filtered %+v %v", c, err)
	}
	c, err = s.Count(ctx, db.RowQuery{ObjRef: ref("orders"), Filters: fs(f("customer_id", "=", "1"))}, false)
	if err != nil || c.N != 2 {
		t.Errorf("partition %+v %v", c, err)
	}
	// estimate: from system.size_estimates when it has data, else falls back to the exact count
	c, err = s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, true)
	if err != nil || (c.Exact && c.N != 3) || c.N < 0 {
		t.Errorf("estimate %+v %v", c, err)
	}
	if _, err := s.Count(ctx, db.RowQuery{ObjRef: ref("nope")}, false); err == nil {
		t.Error("missing table")
	}
}

func TestIntegrationExec(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	r, err := s.Exec(ctx, "SELECT id FROM orders", 2)
	if err != nil || len(r.Rows) != 2 || !r.Truncated || !r.HasRows {
		t.Errorf("limit: %+v %v", r, err)
	}
	r, _ = s.Exec(ctx, "SELECT id FROM orders", 3)
	if r.Truncated || len(r.Rows) != 3 {
		t.Errorf("exactly the limit is not truncated: %+v", r)
	}
	if r, _ = s.Exec(ctx, "SELECT id FROM orders", 0); len(r.Rows) != 3 {
		t.Errorf("unlimited: %+v", r)
	}
	tbl := tempTable(t, s)
	r = exec(t, s, "CREATE TABLE "+q(tbl)+" (id int PRIMARY KEY, v text)")
	if r.HasRows || r.Message != "OK" {
		t.Errorf("DDL result %+v", r)
	}
	r = exec(t, s, "INSERT INTO "+q(tbl)+" (id, v) VALUES (1, 'a')")
	if r.HasRows || r.Message != "OK" {
		t.Errorf("DML result %+v", r)
	}
	// lightweight transactions pass through as ordinary statements and answer with [applied]
	r = exec(t, s, "INSERT INTO "+q(tbl)+" (id, v) VALUES (1, 'b') IF NOT EXISTS")
	if !r.HasRows || r.Columns[0].Name != "[applied]" || r.Rows[0][0] != false || r.Rows[0][2] != "a" {
		t.Errorf("LWT %+v %v", r, r.Rows)
	}
	r = exec(t, s, "UPDATE "+q(tbl)+" SET v = 'c' WHERE id = 1 IF v = 'a'")
	if r.Rows[0][0] != true {
		t.Errorf("LWT update %+v", r.Rows)
	}
	// a batch is one statement for Split and for the server
	exec(t, s, "BEGIN BATCH INSERT INTO "+q(tbl)+" (id, v) VALUES (2, 'x;y'); INSERT INTO "+q(tbl)+" (id, v) VALUES (3, 'z'); APPLY BATCH")
	if r := exec(t, s, "SELECT count(*) FROM "+q(tbl)); r.Rows[0][0] != int64(3) {
		t.Errorf("batch rows %v", r.Rows)
	}
	if r := exec(t, s, "DESCRIBE TABLE "+q(tbl)); !r.HasRows || !strings.Contains(db.FormatValue(r.Rows[0][3]), "CREATE TABLE") {
		t.Errorf("describe %+v", r)
	}
	if _, err := s.Exec(ctx, "SELEC 1", 0); err == nil {
		t.Error("syntax error must surface")
	}
	if _, err := s.Exec(ctx, "SELECT * FROM nope", 0); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown table: %v", err)
	}
	// DDL invalidates the column-type cache: a column added behind the cache must be seen by the next edit
	if _, err := s.colTypes(ctx, ref(tbl)); err != nil {
		t.Fatal(err)
	}
	exec(t, s, "ALTER TABLE "+q(tbl)+" ADD w int")
	if ty, _ := s.colTypes(ctx, ref(tbl)); ty["w"] == nil {
		t.Error("types cache not invalidated by ALTER")
	}
}

func TestIntegrationCancel(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Exec(ctx, "SELECT * FROM system.size_estimates", 0); err == nil {
		t.Error("a cancelled context must fail the statement")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Microsecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := s.Rows(ctx, db.RowQuery{ObjRef: ref("customers")}); err == nil {
		t.Error("an expired deadline must fail Rows")
	}
	if _, err := s.Count(ctx, db.RowQuery{ObjRef: ref("customers")}, false); err == nil {
		t.Error("an expired deadline must fail Count")
	}
	// the session is still usable afterwards
	if _, err := s.Exec(context.Background(), "SELECT * FROM customers", 0); err != nil {
		t.Fatal(err)
	}
	// a driver timeout (option) fails a slow statement instead of hanging
	c := testConn(t)
	c.Options = map[string]string{"timeout": "1ms"}
	if s2, err := (drv{}).Open(context.Background(), c, nil); err == nil {
		defer s2.Close()
		if _, err := s2.Exec(context.Background(), "SELECT * FROM customers", 0); err == nil {
			t.Log("1ms timeout was not hit (fast local query)")
		}
	}
}

func TestIntegrationConcurrent(t *testing.T) {
	s := open(t)
	errs := make(chan error, 16)
	for range 16 {
		go func() {
			_, err := s.Rows(context.Background(), db.RowQuery{ObjRef: ref("customers"), Limit: 10})
			if err == nil {
				err = s.Ping(context.Background())
			}
			errs <- err
		}()
	}
	for range 16 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
