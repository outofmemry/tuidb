package ui

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "webtuidb/internal/db/dynamodb"
)

// End-to-end against DynamoDB Local: PartiQL in the editor, browse and edit a table in the grid,
// commit (ExecuteTransaction), edit a query result in place, and the structure editor's honest refusal.
// Run with WEBTUIDB_INTEGRATION=1 (docker compose up -d --wait dynamodb).
func TestE2EDynamoDB(t *testing.T) {
	h := newRealHarness(t, "WEBTUIDB_TEST_URL_DYNAMODB", "dynamodb://webtuidb:webtuidb@127.0.0.1:58000/?region=us-east-1")
	tbl := fmt.Sprintf("webtuidb_e2e_%d", time.Now().UnixNano()%1_000_000)
	h.press("enter")
	if h.a.ws() == nil {
		t.Fatalf("connect failed:\n%s", h.screen())
	}
	sess := h.a.ws().sess
	t.Cleanup(func() { sess.Exec(context.Background(), fmt.Sprintf(`DROP TABLE "%s"`, tbl), 0) })
	if _, err := sess.Exec(context.Background(), fmt.Sprintf(`CREATE TABLE "%s" {"keys":[{"name":"id","type":"S"}]}`, tbl), 0); err != nil {
		t.Fatal(err)
	}

	// 1. fill the table through the PartiQL editor
	h.prefix("e")
	h.typeText(fmt.Sprintf(`insert into "%s" value {'id': 'a', 'v': 'x', 'n': 1}; insert into "%s" value {'id': 'b', 'v': 'y', 'n': 2}`, tbl, tbl))
	h.prefix("alt+enter")
	h.want("Query 2")
	if got := h.query(fmt.Sprintf(`select id from "%s"`, tbl)); len(got) != 2 {
		t.Fatalf("rows after insert: %v\n%s", got, h.screen())
	}

	// 2. open it from the sidebar, edit a cell, commit
	h.prefix("r")     // reload sidebar
	h.prefix("alt+h") // focus sidebar
	h.press("/")
	h.typeText(tbl)
	h.press("enter", "enter")
	h.want("│y")
	goTo(h.a.ws().tab().(*tableTab).grid, "b", "v") // a Scan returns items in hash order
	h.press("e")
	h.typeText("Z")
	h.press("enter")
	h.prefix("s")
	h.want("committed 1 statement")
	if got := h.query(fmt.Sprintf(`select v from "%s" where id = 'b'`, tbl)); len(got) != 1 || got[0] != "yZ" {
		t.Fatalf("committed value: %v\n%s", got, h.screen())
	}

	// 3. insert a row through the grid (a number column keeps its type)
	h.prefix("i")
	h.press("home", "e") // first column of the new row: id
	h.typeText("c")
	h.press("enter", "l", "e") // columns are id, n, v: the n column
	h.typeText("3")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf(`select n from "%s" where id = 'c'`, tbl)); len(got) != 1 || got[0] != "3" {
		t.Fatalf("inserted row: %v\n%s", got, h.screen())
	}
	if res, _ := sess.Exec(context.Background(), fmt.Sprintf(`select n from "%s" where id = 'c'`, tbl), 0); res == nil || res.Columns[0].Type != "N" {
		t.Errorf("inserted number was stored as %+v", res)
	}

	// 4. delete it again
	goTo(h.a.ws().tab().(*tableTab).grid, "c", "id")
	h.prefix("delete")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf(`select id from "%s"`, tbl)); len(got) != 2 || got[0] == "c" || got[1] == "c" {
		t.Fatalf("rows after delete: %v", got)
	}

	// 5. the structure editor refuses what DynamoDB cannot do, with the reason
	h.prefix(")")
	h.want("column_name")
	h.prefix("i")
	h.press("enter", "ctrl+u")
	h.typeText("extra")
	h.press("enter")
	h.prefix("s")
	h.want("cannot be added")

	// 6. a single-item SELECT in the editor is editable in place
	h.prefix("e")
	h.typeText(fmt.Sprintf(`select * from "%s" where id = 'a'`, tbl))
	h.prefix("alt+enter")
	h.want("Query 1")
	h.prefix("alt+j")
	goTo(h.a.ws().tab().(*queryTab).curResult().grid, "a", "v")
	h.press("e")
	h.typeText("Q")
	h.press("enter")
	h.prefix("s")
	h.want("committed")
	if got := h.query(fmt.Sprintf(`select v from "%s" where id = 'a'`, tbl)); len(got) != 1 || got[0] != "xQ" {
		t.Fatalf("edited result row: %v\n%s", got, h.screen())
	}
}

// goTo puts the grid cursor on the row whose id is id, in column col.
func goTo(g *Grid, id, col string) {
	for i, r := range g.Rows {
		if r.Cur[0] == id {
			g.GoRow(i, false)
		}
	}
	for i, c := range g.Cols {
		if c.Name == col {
			g.GoCol(i, false)
		}
	}
}
