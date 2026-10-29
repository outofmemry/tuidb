package ui

import (
	"errors"
	"strings"
	"testing"
	"time"
	"webtuidb/internal/db"

	tea "charm.land/bubbletea/v2"
)

func feed(o Overlay, msgs ...tea.Msg) (Overlay, []tea.Cmd) {
	var cmds []tea.Cmd
	for _, m := range msgs {
		var c tea.Cmd
		o, c = o.Update(m)
		cmds = append(cmds, c)
		if o == nil {
			break
		}
	}
	return o, cmds
}

func txt(s string) []tea.Msg {
	var out []tea.Msg
	for _, c := range s {
		out = append(out, tea.KeyPressMsg{Code: c, Text: string(c)})
	}
	return out
}

func TestConfirm(t *testing.T) {
	ran := 0
	mk := func() *confirmDlg {
		return &confirmDlg{title: "t", lines: []string{"DELETE FROM x"}, onYes: func() tea.Cmd { ran++; return nil }}
	}
	o, _ := feed(mk(), tea.KeyPressMsg{Code: 'y', Text: "y"})
	if o != nil || ran != 1 {
		t.Errorf("y: %v %d", o, ran)
	}
	o, _ = feed(mk(), tea.KeyPressMsg{Code: tea.KeyEscape})
	if o != nil || ran != 1 {
		t.Error("esc must not run")
	}
	o, _ = feed(mk(), tea.KeyPressMsg{Code: 'x', Text: "x"})
	if o == nil {
		t.Error("other keys keep the dialog open")
	}
}

func TestFormFlow(t *testing.T) {
	var got map[string]string
	f := newForm("New", []formField{
		{Key: "name", Label: "Name", Kind: fText},
		{Key: "pw", Label: "Password", Kind: fSecret},
		{Key: "ssl", Label: "SSL", Kind: fChoice, Value: "prefer", Options: []string{"prefer", "require", "disable"}},
		{Key: "on", Label: "On", Kind: fBool, Value: "false"},
		{Key: "adv", Label: "Adv", Kind: fText, Show: func(v map[string]string) bool { return v["on"] == "true" }},
	}, func(v map[string]string) tea.Cmd { got = v; return nil })
	f.validate = func(v map[string]string) error {
		if v["name"] == "" {
			return errors.New("name required")
		}
		return nil
	}
	var o Overlay = f
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyEnter})
	if o == nil || f.err != "name required" {
		t.Fatalf("validation: %v %q", o, f.err)
	}
	o, _ = feed(o, txt("acme")...)
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyTab})
	o, _ = feed(o, txt("s3cret")...)
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyRight})
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: ' ', Text: " "})
	if f.idx != 3 || f.fields[3].Value != "true" {
		t.Fatalf("bool toggle: idx %d %q", f.idx, f.fields[3].Value)
	}
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyTab}) // the dependent field is now visible
	if f.idx != 4 {
		t.Errorf("conditional field should be reachable, idx %d", f.idx)
	}
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyEnter})
	if o != nil || got["name"] != "acme" || got["pw"] != "s3cret" || got["ssl"] != "require" || got["on"] != "true" {
		t.Errorf("submit: %v %+v", o, got)
	}
	view := newForm("x", []formField{{Key: "p", Label: "P", Kind: fSecret, Value: "hunter2"}}, nil).View(NewTheme(configTheme()), 80, 24)
	if strings.Contains(view, "hunter2") {
		t.Error("secret leaked into the rendered form")
	}
}

func TestPicker(t *testing.T) {
	var picked string
	p := &pickerDlg{title: "x", filter: true, items: []pickItem{{Label: "customers"}, {Label: "orders"}, {Label: "order_items"}},
		onPick: func(it pickItem, ctrl bool) tea.Cmd { picked = it.Label; return nil }}
	var o Overlay = p
	o, _ = feed(o, txt("ord")...)
	if len(p.shown()) != 2 {
		t.Fatalf("filter: %+v", p.shown())
	}
	o, _ = feed(o, tea.KeyPressMsg{Code: tea.KeyEnter})
	if o != nil || picked != "orders" {
		t.Errorf("best match first: %q", picked)
	}
	dyn := &pickerDlg{title: "d", filter: true, onPick: func(pickItem, bool) tea.Cmd { return nil }}
	dyn.dynamic = func(q string) []pickItem { return []pickItem{{Label: "r:" + q}} }
	dyn.items = dyn.dynamic("")
	feed(dyn, txt("ab")...)
	if dyn.shown()[0].Label != "r:ab" {
		t.Errorf("dynamic: %+v", dyn.shown())
	}
	empty := &pickerDlg{title: "e", onPick: func(pickItem, bool) tea.Cmd { t.Error("nothing to pick"); return nil }}
	feed(empty, tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = empty.View(NewTheme(configTheme()), 80, 24)
}

func TestTextViewAndComposite(t *testing.T) {
	ran := false
	tv := &textViewDlg{title: "Preview", lines: []string{strings.Repeat("x", 300)}, actions: []viewAction{{Key: "enter", Label: "Enter: commit", Do: func() tea.Cmd { ran = true; return nil }}}}
	if v := tv.View(NewTheme(configTheme()), 60, 20); width(strings.Split(v, "\n")[0]) > 60 {
		t.Errorf("view wider than max: %d", width(strings.Split(v, "\n")[0]))
	}
	o, _ := feed(tv, tea.KeyPressMsg{Code: tea.KeyEnter})
	if o != nil || !ran {
		t.Error("action")
	}
	base := strings.Repeat(strings.Repeat(".", 30)+"\n", 9) + strings.Repeat(".", 30)
	out := composite(base, "AB\nCD", 30, 10)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 || width(lines[0]) != 30 || !strings.Contains(out, "AB") {
		t.Errorf("composite shape: %d lines", len(lines))
	}
	for _, l := range lines {
		if width(l) != 30 {
			t.Errorf("width %d: %q", width(l), l)
		}
	}
}

func TestChecklistRemembersTickOrder(t *testing.T) {
	c := &checklistDlg{items: []checkItem{{Label: "a"}, {Label: "b"}, {Label: "c"}}}
	c.sel = 2
	c.Update(keyMsg("space"))
	c.sel = 0
	c.Update(keyMsg("space"))
	if c.items[2].Seq >= c.items[0].Seq || c.items[2].Seq == 0 {
		t.Fatalf("tick order lost: %+v", c.items)
	}
}

func TestM13ChecklistUncheckAll(t *testing.T) {
	c := &checklistDlg{items: []checkItem{{Label: "a", Checked: true}, {Label: "b"}}}
	c.Update(keyMsg("A"))
	if c.items[0].Checked || c.items[1].Checked {
		t.Fatal("A unchecks all")
	}
	c.Update(keyMsg("a"))
	if !c.items[0].Checked || !c.items[1].Checked {
		t.Fatal("a checks all")
	}
}

func TestConfirmIgnoresKeysWhileArming(t *testing.T) {
	ran := 0
	d := &confirmDlg{armed: time.Now().Add(time.Hour), onYes: func() tea.Cmd { ran++; return nil }}
	if o, _ := feed(d, tea.KeyPressMsg{Code: 'y', Text: "y"}); o == nil || ran != 0 {
		t.Fatal("y inside the arming window must not approve")
	}
	d.armed = time.Now().Add(-time.Second)
	if o, _ := feed(d, tea.KeyPressMsg{Code: 'y', Text: "y"}); o != nil || ran != 1 {
		t.Fatal("y after the window approves")
	}
}

// T-P3-04: a validation error keeps the dialog open with what was typed.
func TestFlowDialogsKeepInputOnError(t *testing.T) {
	h := newHarness(t, nil)
	h.press("x") // export connections
	setField(t, h, "pass", "")
	setChoice(t, h, "pw", "true")
	h.press("enter")
	h.want("include passwords needs a passphrase")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok {
		t.Fatal("the export form closed on the error")
	}
	h.press("esc")
	h.connect()
	h.openCustomers()
	h.prefix("L")
	h.press("ctrl+u")
	h.typeText("0")
	h.press("enter")
	h.want("limit must be 1 or more")
	if f, ok := h.a.overlays[len(h.a.overlays)-1].(*formDlg); !ok || f.values()["limit"] != "0" {
		t.Fatal("the page settings form closed on the error")
	}
	h.press("esc")
	h.pump(h.a.ws().importWizard(db.ObjRef{Name: "customers", Kind: db.KindTable}, false))
	setField(t, h, "path", "/nonexistent/x.csv")
	setField(t, h, "nullas", "NA")
	h.press("enter")
	h.want("cannot read that file")
	h.press("esc")
	h.pump(h.a.ws().importWizard(db.ObjRef{Name: "customers", Kind: db.KindTable}, false))
	if v := h.a.overlays[len(h.a.overlays)-1].(*formDlg).values(); v["path"] != "/nonexistent/x.csv" || v["nullas"] != "NA" {
		t.Fatalf("the import dialog forgot its answers: %v", v)
	}
	if savedQueryName(map[string]string{"name": "reports/ "}) == nil || savedQueryName(map[string]string{"name": "q"}) != nil {
		t.Fatal("savedQueryName")
	}
}
