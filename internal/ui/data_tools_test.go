package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestSchemaDiffTextCommentsDropsOut(t *testing.T) {
	keep := []db.Statement{{Text: "CREATE TABLE a (id int)"}}
	drops := []db.Statement{{Text: `DROP TABLE "b"`}}
	off := schemaDiffText(keep, drops, false)
	if !strings.Contains(off, "-- DROP TABLE \"b\";") || strings.Contains(off, "\nDROP TABLE") || !strings.Contains(off, "commented out below") {
		t.Fatalf("drops must be commented out:\n%s", off)
	}
	on := schemaDiffText(keep, drops, true)
	if !strings.Contains(on, "\nDROP TABLE \"b\";") {
		t.Fatalf("drops requested:\n%s", on)
	}
}

func TestConfirmBulkAsksForDeletes(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	w := h.a.ws()
	ran := 0
	apply := func() tea.Cmd { ran++; return nil }
	w.confirmBulk("t", []db.Statement{{Text: "INSERT INTO t VALUES (1)"}}, apply)
	if ran != 1 {
		t.Fatal("a small insert batch must run at once")
	}
	w.confirmBulk("t", []db.Statement{{Text: "DELETE FROM t WHERE id = 1"}}, apply)
	if ran != 1 {
		t.Fatal("a delete batch must wait for the typed confirmation")
	}
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if f.validate(map[string]string{"confirm": "2"}) == nil || f.validate(map[string]string{"confirm": "1"}) != nil {
		t.Fatal("the count is the confirmation")
	}
}

func TestM5BulkUpdateThroughTheGate(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.openCustomers()
	h.run("bulk_update")
	h.want("filter the rows first")
	h.a.ws().tab().(*tableTab).filters = db.FilterSet{Filters: []db.Filter{{Column: "name", Op: "=", Value: "Bob", Enabled: true}}}
	h.a.ws().tab().(*tableTab).markApplied()
	h.run("bulk_update")
	setField(t, h, "set", "email=x@y.z ;; name=NULL")
	h.press("enter")
	h.want("Bulk update") // the preview comes first
	h.press("a")
	h.want("send to the server?") // the safe-mode gate sees the UPDATE
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing is sent before confirming")
	}
	h.press("y")
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 1 || !strings.HasPrefix(h.fake.applied[0][0].Text, `UPDATE "public"."customers" SET "email" = $1, "name" = NULL WHERE`) {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM5FakeRows(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("fake_rows")
	setField(t, h, "n", "3")
	h.press("enter")
	h.press("a") // preview, then apply
	if len(h.fake.applied) != 1 || len(h.fake.applied[0]) != 3 || !strings.HasPrefix(h.fake.applied[0][0].Text, "INSERT") {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM5Diffs(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("schema_diff")
	setField(t, h, "to", "app")
	h.press("enter")
	if q, ok := h.a.ws().tab().(*queryTab); !ok || !strings.Contains(q.ed.Text(), "ALTER TABLE customers;") { // the fake plans an ALTER for every table
		t.Fatalf("schema diff must land in an editor tab:\n%s", h.screen())
	}
	h.run("data_diff")
	h.press("enter") // customers -> orders: the fake serves the same rows for both
	h.want("nothing to change")
}

func TestCheckExpr(t *testing.T) {
	for _, ok := range []string{"now()", "coalesce(a, 'it''s')"} {
		if err := checkExpr(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"1; DROP TABLE t", "1 -- x", "1 /* x", "$$x$$", "'abc", "(1", "1)"} {
		if checkExpr(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// T-P3-08 + T-P3-22: a preview shows tabs as → and applies with Enter; the bulk update says how many rows match.
func TestFlowBulkUpdatePreviewEnterAndCount(t *testing.T) {
	h := newHarness(t, func(c *config.Connection, _ *config.Config) { c.SafeMode = "alert2" })
	h.connect()
	h.pump(h.a.ws().previewStatements("Fix", []db.Statement{{Text: "UPDATE t SET a = 'multi\nline\tnote'"}}, nil))
	h.want("line→note")
	h.press("esc")
	h.openCustomers()
	h.prefix("f")
	h.typeText("2")
	h.press("enter", "esc")
	h.run("bulk_update")
	h.want("3 rows match the filter") // the fake counts every row
	setField(t, h, "set", "email=x@y.z")
	h.press("enter") // the preview
	h.press("enter") // applies it, through the safe-mode gate
	h.want("send to the server?")
}
