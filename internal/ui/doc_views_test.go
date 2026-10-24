package ui

import (
	"strings"
	"testing"

	"tuidb/internal/db"
)

func TestParseDocKeepsOrderAndNesting(t *testing.T) {
	n, err := parseDoc(`{"z":1,"a":{"m":[1,"x",null,true]},"b":2.50}`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, k := range n.kids {
		keys = append(keys, k.key)
	}
	if strings.Join(keys, ",") != "z,a,b" {
		t.Errorf("order not preserved: %v", keys)
	}
	arr := n.kids[1].kids[0]
	if arr.kind != 'a' || len(arr.kids) != 4 || arr.kids[1].val != `"x"` || arr.kids[2].val != "null" || arr.kids[3].val != "true" {
		t.Errorf("array: %+v", arr)
	}
	if n.kids[2].val != "2.50" {
		t.Errorf("numbers keep their text: %q", n.kids[2].val)
	}
	for _, bad := range []string{`{"a":`, `{"a":1} x`, ``} {
		if _, err := parseDoc(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestJSONKinds(t *testing.T) {
	line := `  "k\"x": "v:", -1.5e3, true, null`
	k := jsonKinds(line)
	r := []rune(line)
	at := func(sub string) tokKind { return k[len([]rune(line[:strings.Index(line, sub)]))] }
	if len(k) != len(r) || at(`"k`) != tkKey || at(`"v`) != tkString || at("-1") != tkNumber || at("true") != tkBool || at("null") != tkNullLit || at(",") != tkPunct {
		t.Errorf("kinds: %v", k)
	}
	if k[strings.Index(line, `x"`)] != tkKey {
		t.Error("an escaped quote does not end the key")
	}
}

func TestDocTreeNavigation(t *testing.T) {
	tr := newDocTree([]string{`{"name":"Ann","tags":["a","b"]}`, `{"name":"Bob"}`})
	if got := len(tr.rows()); got != 2+1+1+0 && got != 5 { // [0] open: name,tags ; [1] open: name
		t.Logf("rows: %d", got)
	}
	rows := tr.rows()
	if len(rows) != 5 {
		t.Fatalf("expected 2 roots + 3 fields visible, got %d", len(rows))
	}
	tr.input("down")
	tr.input("down") // "tags"
	if tr.rows()[tr.sel].node.key != "tags" {
		t.Fatalf("selected %q", tr.rows()[tr.sel].node.key)
	}
	tr.input("enter") // expand
	if len(tr.rows()) != 7 {
		t.Errorf("expanding tags shows its 2 items: %d rows", len(tr.rows()))
	}
	tr.input("left") // collapse
	if len(tr.rows()) != 5 {
		t.Errorf("collapse: %d rows", len(tr.rows()))
	}
	tr.input("C")
	if len(tr.rows()) != 2 {
		t.Errorf("collapse all leaves the roots: %d", len(tr.rows()))
	}
	tr.input("E")
	if len(tr.rows()) != 7 {
		t.Errorf("expand all: %d", len(tr.rows()))
	}
	if v := tr.view(NewTheme(configTheme()), 50, 4); len(strings.Split(v, "\n")) != 4 {
		t.Error("view height")
	}
}

func TestFlowDocumentViews(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.document = true
	h.connect()
	h.openCustomers()
	h.want("Json")
	h.want("Tree")
	h.prefix("J")
	h.want(`"name": "Ann"`)
	h.want(`"city": "Oslo"`)
	h.prefix("T")
	h.want("{4}")
	h.press("down", "down", "down", "enter") // expand "tags"... whichever composite is selected
	h.prefix("(")                            // back to data
	h.want("ann@x.io")
	// SQL engines have no document views
	h2 := newHarness(t, nil)
	h2.connect()
	h2.openCustomers()
	h2.wantNot("Tree")
	h2.prefix("J")
	h2.want("document engines")
}

func TestEditableDDLAndTriggerSQL(t *testing.T) {
	fn := db.ObjRef{Schema: "public", Name: "f", Kind: db.KindFunction}
	if got := editableDDL("postgres", fn, "CREATE OR REPLACE FUNCTION f() ..."); strings.Contains(got, "DROP") || !strings.Contains(got, "CREATE OR REPLACE FUNCTION f()") {
		t.Errorf("postgres functions replace in place: %q", got)
	}
	if got := editableDDL("mysql", db.ObjRef{Name: "f", Kind: db.KindFunction}, "CREATE DEFINER=`u`@`%` FUNCTION `f`() RETURNS int BEGIN RETURN 1; END"); !strings.Contains(got, "DELIMITER $$\nCREATE FUNCTION `f__tuidb_new`() RETURNS int BEGIN RETURN 1; END$$\nDROP FUNCTION IF EXISTS `f`$$") || strings.Index(got, "DROP") < strings.Index(got, "CREATE") {
		t.Errorf("mysql routines are created under a new name first, then the old one dropped, with a DELIMITER: %q", got)
	}
	if got := editableDDL("mysql", db.ObjRef{Name: "v", Kind: db.KindView}, "CREATE ALGORITHM=UNDEFINED VIEW v AS select 1"); !strings.Contains(got, "CREATE OR REPLACE ALGORITHM") {
		t.Errorf("mysql views: %q", got)
	}
	if got := editableDDL("postgres", db.ObjRef{Schema: "s", Name: "m", Kind: db.KindMatView}, "CREATE MATERIALIZED VIEW m AS select 1"); !strings.Contains(got, `DROP MATERIALIZED VIEW IF EXISTS "s"."m";`) {
		t.Errorf("matviews are dropped first: %q", got)
	}
	tr := db.Trigger{Name: "audit", Timing: "AFTER", Event: "INSERT", Statement: "FOR EACH ROW EXECUTE FUNCTION log()"}
	pg := triggerSQL("postgres", db.ObjRef{Schema: "public", Name: "orders"}, tr)
	if !strings.HasPrefix(pg, `DROP TRIGGER IF EXISTS "audit" ON "public"."orders";`) || !strings.Contains(pg, `CREATE TRIGGER "audit" AFTER INSERT ON "public"."orders"`) || !strings.Contains(pg, "EXECUTE FUNCTION log();") {
		t.Errorf("postgres trigger: %q", pg)
	}
	my := triggerSQL("mysql", db.ObjRef{Name: "orders"}, db.Trigger{Name: "a", Timing: "AFTER", Event: "INSERT", Statement: "SET @x = 1"})
	if !strings.Contains(my, "DROP TRIGGER IF EXISTS `a`;") || !strings.Contains(my, "ON `orders` FOR EACH ROW\n  SET @x = 1;") {
		t.Errorf("mysql trigger: %q", my)
	}
	if triggerSQL("redis", db.ObjRef{}, tr) != "" || newTriggerSQL("mongo", db.ObjRef{}) != "" {
		t.Error("engines without triggers produce nothing")
	}
}

func TestQuickLookTreeForJSONRawForText(t *testing.T) {
	q := newQuickLook("meta", `{"tier":"gold","tags":["a","b"]}`)
	if q.tree == nil || q.raw {
		t.Fatal("JSON opens as a tree")
	}
	th := NewTheme(configTheme())
	if v := stripANSI(q.View(th, 80, 24)); !strings.Contains(v, "tier") || !strings.Contains(v, "{2}") {
		t.Errorf("tree view: %s", v)
	}
	q.Update(keyMsg("r"))
	if v := stripANSI(q.View(th, 80, 24)); !strings.Contains(v, `"tier": "gold"`) {
		t.Errorf("r shows pretty raw text: %s", v)
	}
	if p := newQuickLook("note", "just some text\nover two lines"); p.tree != nil || !p.raw {
		t.Error("plain text stays plain")
	}
	if p := newQuickLook("bad", `{"a": `); p.tree != nil {
		t.Error("broken JSON falls back to text")
	}
	if o, _ := q.Update(keyMsg("esc")); o != nil {
		t.Error("esc closes")
	}
}

func TestJSONViewSurvivesReloadToZeroDocuments(t *testing.T) {
	tt := &tableTab{docs: []string{`{"a":1}`}}
	th := NewTheme(configTheme())
	_ = tt.jsonView(th, 40, 5)
	tt.docs = nil
	_ = tt.jsonView(th, 40, 5) // used to index docs[0] of an empty slice
}
