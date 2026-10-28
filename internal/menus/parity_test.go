package menus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTree concatenates the non-test sources under dir (relative to the repo root) with one of the extensions.
func readTree(t *testing.T, dir string, exts ...string) string {
	t.Helper()
	var b strings.Builder
	filepath.WalkDir(filepath.Join("..", "..", dir), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		for _, e := range exts {
			if !d.IsDir() && strings.HasSuffix(p, e) && !strings.HasSuffix(p, "_test.go") {
				if c, err := os.ReadFile(p); err == nil {
					b.Write(c)
					b.WriteByte('\n')
				}
			}
		}
		return nil
	})
	if b.Len() == 0 {
		t.Fatalf("no %v files under %s", exts, dir)
	}
	return b.String()
}

// TestParityDialogs (M14 check 6): every dialog pair in Dialogs exists in both UIs.
func TestParityDialogs(t *testing.T) {
	tui := readTree(t, "internal/ui", ".go")
	web := readTree(t, "web/src", ".ts", ".tsx")
	seen := map[string]bool{}
	for _, d := range Dialogs {
		if seen[d.Name] {
			t.Errorf("dialog %q is listed twice", d.Name)
		}
		seen[d.Name] = true
		if d.TUI == "" || d.Web == "" {
			t.Errorf("dialog %q: both a TUI and a Web symbol are required (name the gap in the report instead of leaving one blank)", d.Name)
			continue
		}
		if !strings.Contains(tui, d.TUI) {
			t.Errorf("dialog %q: internal/ui has no %q: the terminal lacks it (the web has %q)", d.Name, d.TUI, d.Web)
		}
		if !strings.Contains(web, d.Web) {
			t.Errorf("dialog %q: web/src has no %q: the web lacks it (the terminal has %q)", d.Name, d.Web, d.TUI)
		}
	}
}
