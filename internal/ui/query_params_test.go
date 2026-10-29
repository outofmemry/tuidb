package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestM2VariablesBindParam(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.QueryParams = true })
	h.connect()
	h.prefix("e")
	h.typeText("select :id")
	h.prefix("alt+enter")
	h.want("Variables")
	h.typeText("7")
	h.press("enter")
	if len(h.fake.execs) != 1 || !strings.Contains(h.fake.execs[0], "7") || strings.Contains(h.fake.execs[0], ":id") {
		t.Fatalf("execs %v", h.fake.execs)
	}
	h.prefix("alt+enter") // the value is remembered
	h.want("Variables")
	h.want("7")
}

func TestM4CleanupVariablesForgetsThem(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.a.ws().vars = map[string]string{"a": "1"}
	h.run("cleanup_variables")
	if h.a.ws().vars != nil {
		t.Fatal("variables must be forgotten")
	}
	h.run("wrap_lines")
	if h.a.ws().tab().(*queryTab).ed.Opts.Wrap {
		t.Fatal("wrap_lines must toggle the default (on) off")
	}
}

func TestM8VariablesFormatPicker(t *testing.T) {
	h := newHarness(t, func(_ *config.Connection, c *config.Config) { c.Editor.QueryParams = true })
	h.connect()
	h.prefix("e")
	q := h.queryTab()
	q.ed.SetText("select :a, @b")
	h.run("run_all")
	h.want("Variables")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if v := f.values(); v[formatKey] != "colon" || v["a"] != "" {
		t.Fatalf("starts on the configured format: %v", v)
	}
	setChoice(t, h, formatKey, "at") // @b is a variable in the at format
	h.press("enter")
	setField(t, h, "b", "5")
	h.press("enter")
	if len(h.fake.execs) != 1 || h.fake.execs[0] != "select :a, 5" {
		t.Fatalf("execs %v", h.fake.execs)
	}
	// the variables action offers whichever format finds variables
	q.ed.SetText("select ${x}")
	h.run("variables")
	if v := h.a.overlays[len(h.a.overlays)-1].(*formDlg).values(); v[formatKey] != "brace" {
		t.Fatalf("format %v", v)
	}
}

// T-P2-10: Enter in the Variables dialog saves the values and runs with them bound, even with query_params off;
// a later run with the values unbound says how to turn parameters on.
func TestFlowVariablesDialogRuns(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("e")
	h.queryTab().ed.SetText("select * from customers where id = :id")
	h.run("variables")
	h.want("Variables")
	h.typeText("2")
	h.press("enter")
	if n := len(h.fake.execs); n == 0 || strings.Contains(h.fake.execs[n-1], ":id") || !strings.Contains(h.fake.execs[n-1], "2") {
		t.Fatalf("the statement must run with :id bound: %v", h.fake.execs)
	}
	h.prefix("enter")
	h.want("Parameters are off")
}
