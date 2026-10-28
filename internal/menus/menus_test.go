package menus

import (
	"reflect"
	"testing"
)

func TestHolds(t *testing.T) {
	env := Env{Kind: "view", Caps: map[string]bool{"Schemas": true}}
	got := Labels(SidebarObject, env)
	want := []string{"Open", "Open in new tab", "New table…", "Open structure", "Edit definition", "Show DDL", "Copy name",
		"Copy script: CREATE", "Copy script: DROP", "Export…", "Export several tables…", "Delete…", "Pin / unpin", "Mark / unmark",
		"Add to group…", "Diagram", "Set colour…", "New schema…", "Drop schema…"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view menu:\n got %q\nwant %q", got, want)
	}
	for _, l := range Labels(SidebarObject, Env{Kind: "table"}) { // Triggers… needs cap:Triggers
		if l == "Triggers…" {
			t.Fatal("Triggers shown without the capability")
		}
	}
	for name, l := range All() {
		seen := map[string]bool{}
		for _, it := range l {
			if it.Label == "" || it.Action == "" {
				t.Errorf("%s: empty label or action in %+v", name, it)
			}
			if seen[it.Label] {
				t.Errorf("%s: duplicate label %q", name, it.Label)
			}
			seen[it.Label] = true
		}
	}
}

// P2-15: "Rename…" follows Capabilities.RenameDB (Vertica turns it off).
func TestDatabaseRenameNeedsTheCapability(t *testing.T) {
	has := func(env Env) bool {
		for _, l := range Labels(Database, env) {
			if l == "Rename…" {
				return true
			}
		}
		return false
	}
	if has(Env{Kind: "database", Caps: map[string]bool{"RenameDB": false}}) {
		t.Fatal("Rename offered without RenameDB")
	}
	if !has(Env{Kind: "database", Caps: map[string]bool{"RenameDB": true}}) {
		t.Fatal("Rename missing with RenameDB")
	}
}
