package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtuidb/internal/db"
	"webtuidb/internal/importer"
)

func TestImportMappingRejectsTwoColumnsOntoOne(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.a.ws().mapColumns("f.csv", importer.Options{Table: db.ObjRef{Name: "t"}}, []string{"email", "name"}, nil, []string{"name", "email"})
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	if err := f.validate(map[string]string{"0": "name", "1": "name"}); err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("duplicate target accepted: %v", err)
	}
	if err := f.validate(map[string]string{"0": "email", "1": "(skip)"}); err != nil {
		t.Fatal(err)
	}
}

// P2-48 / P2-59: the import dialog has the NULL-handling fields.
func TestTUI1ImportDialogHasNullFields(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("import")
	seen := map[string]string{}
	for _, fl := range h.a.overlays[len(h.a.overlays)-1].(*formDlg).fields {
		seen[fl.Key] = fl.Label
	}
	if seen["nullas"] != "NULL as" || seen["emptynull"] != "Empty is NULL" || seen["keepquote"] != "Keep formula apostrophe" {
		t.Fatalf("fields %v", seen)
	}
	_ = os.Stdout
}

func TestTUI3ImportNullAsHint(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.run("import")
	f := h.a.overlays[len(h.a.overlays)-1].(*formDlg)
	for _, fl := range f.fields {
		if fl.Key == "nullas" {
			if !strings.Contains(fl.Hint, "[csv] null_as") {
				t.Fatalf("hint %q", fl.Hint)
			}
			return
		}
	}
	t.Fatal("no nullas field")
}

func TestM2ImportCSVRunsInserts(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.openCustomers()
	path := filepath.Join(t.TempDir(), "in.csv")
	os.WriteFile(path, []byte("id,name\n9,Zed\n"), 0o644)
	h.prefix("I")
	h.want("Import data")
	setField(t, h, "path", path)
	h.press("enter")
	h.want("Column mapping")
	h.press("enter")
	h.want("processed 1 rows")
	if len(h.fake.applied) == 0 {
		t.Fatal("nothing was applied")
	}
}
