package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/config"
	"tuidb/internal/db"
	"tuidb/internal/store"

	_ "tuidb/internal/db/mysql"
	_ "tuidb/internal/db/postgres"
)

// End-to-end: real UI model, real driver, real database. Run with TUIDB_INTEGRATION=1 (make up first).
func newRealHarness(t *testing.T, urlEnv, def string) *harness {
	t.Helper()
	if os.Getenv("TUIDB_INTEGRATION") == "" {
		t.Skip("set TUIDB_INTEGRATION=1 (and `make up`) to run")
	}
	u := os.Getenv(urlEnv)
	if u == "" {
		u = def
	}
	conn, err := config.ParseURL(u)
	if err != nil {
		t.Fatal(err)
	}
	conn.Name = "e2e"
	dir := t.TempDir()
	cfg := config.Defaults()
	km, _ := cfg.Keymap()
	state, _ := store.LoadState(filepath.Join(dir, "state.json"))
	d := &Deps{Cfg: cfg, Keys: km, Conns: []config.Connection{conn}, ConnPath: filepath.Join(dir, "c.toml"), State: state,
		Hist: &store.History{Dir: filepath.Join(dir, "h")}, Queries: &store.Queries{Root: filepath.Join(dir, "q")}, NoTimers: true, Connect: DefaultConnect}
	h := &harness{t: t, a: NewApp(d), wait: 20 * time.Second}
	h.a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return h
}

// query runs sql on the harness's own session and returns the first column of every row.
func (h *harness) query(sql string) []string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := h.a.ws().sess.Exec(ctx, sql, 0)
	if err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
	var out []string
	for _, r := range res.Rows {
		var cells []string
		for _, c := range r {
			cells = append(cells, db.FormatValue(c))
		}
		out = append(out, strings.Join(cells, "|"))
	}
	return out
}

func e2eSQL(t *testing.T, name, urlEnv, def string) {
	h := newRealHarness(t, urlEnv, def)
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

	// 1. create + fill the table through the SQL editor
	h.prefix("e")
	h.typeText(fmt.Sprintf("create table %s (id int primary key, v varchar(50)); insert into %s values (1, 'a'), (2, 'b')", tbl, tbl))
	h.prefix("alt+enter")
	h.want("Query 2")
	if got := h.query("select count(*) from " + tbl); got[0] != "2" {
		t.Fatalf("%s: rows after insert: %v", name, got)
	}

	// 2. open it from the sidebar, edit a cell, commit
	h.prefix("r")     // reload sidebar
	h.prefix("alt+h") // focus sidebar
	h.press("/")
	h.typeText(tbl)
	h.press("enter", "enter")
	h.want("│b")
	h.press("j", "l", "enter")
	h.typeText("Z")
	h.press("enter")
	h.prefix("s")
	h.want("committed 1 statement")
	if got := h.query(fmt.Sprintf("select v from %s where id = 2", tbl)); got[0] != "bZ" {
		t.Fatalf("%s: committed value: %v\n%s", name, got, h.screen())
	}

	// 3. insert a row through the grid
	h.prefix("i")
	h.press("home", "enter") // first column of the new row: id
	h.typeText("3")
	h.press("enter", "l", "enter") // then v
	h.typeText("c")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf("select v from %s where id = 3", tbl)); len(got) != 1 || got[0] != "c" {
		t.Fatalf("%s: inserted row: %v\n%s", name, got, h.screen())
	}

	// 4. delete it again
	h.press("G")
	h.prefix("delete")
	h.prefix("s")
	h.want("committed")
	if got := h.query("select count(*) from " + tbl); got[0] != "2" {
		t.Fatalf("%s: rows after delete: %v", name, got)
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
	if got := h.query(fmt.Sprintf("select count(*) from information_schema.columns where table_name = '%s' and column_name = 'extra'", tbl)); got[0] != "1" {
		t.Fatalf("%s: column not added: %v\n%s", name, got, h.screen())
	}

	// 6. edit a query result in place: a plain single-table SELECT with its key is editable
	h.prefix("e")
	h.typeText("select * from " + tbl + " order by id")
	h.prefix("alt+enter")
	h.want("Query 1")
	h.prefix("alt+j")
	g := h.a.ws().tab().(*queryTab).curResult().grid
	for i, c := range g.Cols { // column order differs per engine (MySQL puts the new column before v)
		if c.Name == "v" {
			g.GoCol(i, false)
		}
	}
	h.press("enter")
	h.typeText("Y")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf("select v from %s where id = 1", tbl)); got[0] != "aY" {
		t.Fatalf("%s: edited result row: %v\n%s", name, got, h.screen())
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
		t.Errorf("%s: history editor=%d gui=%d", name, editor, gui)
	}
}

func TestE2EPostgres(t *testing.T) {
	e2eSQL(t, "postgres", "TUIDB_TEST_URL_POSTGRES", "postgresql://tuidb:tuidb@127.0.0.1:55432/tuidb")
}

func TestE2EMySQL(t *testing.T) {
	e2eSQL(t, "mysql", "TUIDB_TEST_URL_MYSQL", "mysql://tuidb:tuidb@127.0.0.1:53306/tuidb")
}
