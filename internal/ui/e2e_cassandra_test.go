package ui

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"tuidb/internal/config"
	"tuidb/internal/db"

	_ "tuidb/internal/db/cassandra"
)

// TestE2ECassandra is e2eSQL for CQL: no multi-row INSERT, no ORDER BY outside a partition, and the table is keyed
// (p, id) so that its rows come back in a known order.
func TestE2ECassandra(t *testing.T) {
	url := os.Getenv("TUIDB_TEST_URL_CASSANDRA")
	if url == "" {
		url = "cassandra://tuidb:tuidb@127.0.0.1:59042/tuidb"
	}
	if os.Getenv("TUIDB_INTEGRATION") != "" { // the keyspace the connection URL names
		c, err := config.ParseURL(url)
		if err != nil {
			t.Fatal(err)
		}
		ks := c.Database
		c.Database = ""
		drv, _ := db.Lookup("cassandra")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s, err := drv.Open(ctx, c, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Exec(ctx, "CREATE KEYSPACE IF NOT EXISTS "+ks+" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}", 0)
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	h := newRealHarness(t, "TUIDB_TEST_URL_CASSANDRA", url)
	tbl := fmt.Sprintf("tuidb_e2e_%d", time.Now().UnixNano()%1_000_000)
	h.press("enter")
	if h.a.ws() == nil {
		t.Fatalf("connect failed:\n%s", h.screen())
	}
	t.Cleanup(func() {
		if ws := h.a.ws(); ws != nil {
			ws.sess.Exec(context.Background(), "drop table if exists "+tbl, 0)
		}
	})

	// 1. create and fill the table through the CQL editor: three statements, one of them a batch
	h.prefix("e")
	h.typeText(fmt.Sprintf("create table %s (p int, id int, v text, primary key (p, id)); insert into %s (p, id, v) values (1, 1, 'a'); "+
		"begin batch insert into %s (p, id, v) values (1, 2, 'b'); insert into %s (p, id, v) values (1, 9, 'z'); apply batch", tbl, tbl, tbl, tbl))
	h.prefix("alt+enter")
	h.want("Query 3")
	if got := h.query("select count(*) from " + tbl); got[0] != "3" {
		t.Fatalf("rows after insert: %v", got)
	}

	// 2. open it from the sidebar, edit a cell, commit
	h.prefix("r")
	h.prefix("alt+h")
	h.press("/")
	h.typeText(tbl)
	h.press("enter", "enter")
	h.want("│b")
	h.press("j", "l", "l", "e") // second row (id 2), column v
	h.typeText("Z")
	h.press("enter")
	h.prefix("s")
	h.want("committed 1 statement")
	if got := h.query(fmt.Sprintf("select v from %s where p = 1 and id = 2", tbl)); got[0] != "bZ" {
		t.Fatalf("committed value: %v\n%s", got, h.screen())
	}

	// 3. insert a row through the grid
	h.prefix("i")
	h.press("home", "e")
	h.typeText("1")
	h.press("enter", "l", "e")
	h.typeText("3")
	h.press("enter", "l", "e")
	h.typeText("c")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf("select v from %s where p = 1 and id = 3", tbl)); len(got) != 1 || got[0] != "c" {
		t.Fatalf("inserted row: %v\n%s", got, h.screen())
	}

	// 4. delete the last row (id 9)
	h.press("G")
	h.prefix("delete")
	h.prefix("s")
	h.want("committed")
	if got := h.query("select count(*) from " + tbl); got[0] != "3" {
		t.Fatalf("rows after delete: %v", got)
	}

	// 5. structure: add a column and commit
	h.prefix(")")
	h.want("column_name")
	h.prefix("i")
	h.press("enter", "ctrl+u")
	h.typeText("extra")
	h.press("enter")
	h.prefix("s")
	h.want("saved")
	if got := h.query(fmt.Sprintf("select count(*) from system_schema.columns where keyspace_name = 'tuidb' and table_name = '%s' and column_name = 'extra'", tbl)); got[0] != "1" {
		t.Fatalf("column not added: %v\n%s", got, h.screen())
	}

	// 6. edit a query result in place: a plain single-table SELECT with its whole key is editable
	h.prefix("e")
	h.typeText("select * from " + tbl + " where p = 1")
	h.prefix("alt+enter")
	h.want("Query 1")
	h.prefix("alt+j")
	g := h.a.ws().tab().(*queryTab).curResult().grid
	for i, c := range g.Cols {
		if c.Name == "v" {
			g.GoCol(i, false)
		}
	}
	h.press("e")
	h.typeText("Y")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf("select v from %s where p = 1 and id = 1", tbl)); got[0] != "aY" {
		t.Fatalf("edited result row: %v\n%s", got, h.screen())
	}

	// 7. history has both editor and GUI statements
	hist, _ := h.a.d.Hist.List("e2e", "", 0)
	var editor, gui int
	for _, e := range hist {
		if e.Source == "editor" {
			editor++
		} else {
			gui++
		}
	}
	if editor < 3 || gui < 4 {
		t.Errorf("history editor=%d gui=%d", editor, gui)
	}
}
