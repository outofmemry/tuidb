package ui

import (
	"strings"
	"testing"

	"tuidb/internal/db"
)

func keys(names ...string) []db.Object {
	var out []db.Object
	for _, n := range names {
		out = append(out, db.Object{ObjRef: db.ObjRef{Name: n, Kind: db.KindTable}, Detail: "string"})
	}
	return out
}

func TestKeyTreeGroupsByColon(t *testing.T) {
	objs := keys("counter", "user:1", "user:1:name", "user:2:name", "user:2:email", "session:abc", "queue:jobs")
	s := &sidebar{open: map[string]bool{}}
	var rows []sbRow
	s.flattenKeys(buildKeyTree(objs), 0, &rows)
	var got []string
	for _, r := range rows {
		got = append(got, strings.Repeat(" ", r.depth)+map[sbKind]string{sbFolder: "+", sbObject: "-"}[r.kind]+r.text+r.label)
	}
	want := "-counter,+queue:jobs,+session:abc,+user"
	_ = want
	// single-key chains collapse: session:abc and queue:jobs are leaves, not folders three levels deep
	for _, w := range []string{"-counter", "-queue:jobs", "-session:abc", "+user"} {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("missing %q in %v", w, got)
		}
	}
	if len(rows) != 4 {
		t.Errorf("collapsed folders hide their keys: %v", got)
	}
	s.open["user"] = true
	rows = nil
	s.flattenKeys(buildKeyTree(objs), 0, &rows)
	var user []string
	for _, r := range rows {
		if r.depth == 1 {
			user = append(user, r.text+r.label)
		}
	}
	// user:1 is both a key and a prefix (user:1:name), user:2 only a prefix of two keys
	if strings.Join(user, ",") != "1,2" {
		t.Errorf("children of user: %v", user)
	}
	for _, r := range rows {
		if r.kind == sbFolder && r.text == "user" && r.count != 4 {
			t.Errorf("user counts all keys below it: %d", r.count)
		}
		if r.kind == sbFolder && r.text == "1" && !r.hasObj {
			t.Error("user:1 is itself a key and must stay openable")
		}
	}
	if rows := (&sidebar{open: map[string]bool{}}); true {
		var none []sbRow
		rows.flattenKeys(buildKeyTree(nil), 0, &none)
		if len(none) != 0 {
			t.Error("no keys, no rows")
		}
	}
}

func TestFlowRedisSidebarFolders(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.editor = "redis"
	h.fake.objs = keys("counter", "user:1:name", "user:1:email", "user:2:name")
	h.press("enter")
	h.want("Keys (4)")
	h.want("counter")
	h.want("user")
	h.wantNot("email") // folders start collapsed
	h.press("j", "j")  // counter -> user
	h.press("enter")   // expand
	h.want("▾ user")
	h.want("▸ 1")
	h.want("2:name") // a lone key under a prefix is a leaf, not a one-item folder
	h.press("h")     // collapse again
	h.want("▸ user")
	h.press("l")
	h.press("j", "l")
	h.want("email")
	h.press("/")
	h.typeText("email")
	h.want("user:1:email") // search lists full key names, flat
}
