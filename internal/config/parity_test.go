package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var paritySections = []string{"general", "editor", "table", "csv", "sidebar", "theme", "export", "web", "queries", "security"}

// readAll concatenates the files under dir with one of the extensions; nil when dir is missing.
func readAll(dir string, exts ...string) []byte {
	var b []byte
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		for _, e := range exts {
			if !d.IsDir() && strings.HasSuffix(p, e) && !strings.HasSuffix(p, "_test.go") {
				if c, err := os.ReadFile(p); err == nil {
					b = append(b, c...)
				}
			}
		}
		return nil
	})
	return b
}

func camel(snake string) string {
	parts := strings.Split(snake, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// TestParityCoversEveryKey (M14 check 3): every config key is listed in Parity, and the UI that Parity says honours
// it really references it.
func TestParityCoversEveryKey(t *testing.T) {
	listed := map[string]ParityKey{}
	for _, p := range Parity {
		listed[p.Key] = p
	}
	// every tagged field is listed
	rt := reflect.TypeOf(Config{})
	fieldOf := map[string]string{} // key -> Go field name
	secOf := map[string]string{}   // key -> Go name of its section struct in Config
	for i := 0; i < rt.NumField(); i++ {
		sec := rt.Field(i).Tag.Get("toml")
		known := false
		for _, s := range paritySections {
			known = known || s == sec
		}
		if !known {
			continue
		}
		st := rt.Field(i).Type
		for j := 0; j < st.NumField(); j++ {
			key := sec + "." + st.Field(j).Tag.Get("toml")
			fieldOf[key] = st.Field(j).Name
			secOf[key] = rt.Field(i).Name
			if _, ok := listed[key]; !ok {
				t.Errorf("config key %s is not in config.Parity", key)
			}
		}
	}
	for k := range listed {
		if _, ok := fieldOf[k]; !ok {
			t.Errorf("config.Parity lists %s, which is not a config key", k)
		}
	}
	root := filepath.Join("..", "..")
	// the terminal UI reads the key itself or through the shared exporter
	ui := append(readAll(filepath.Join(root, "internal", "ui"), ".go"), readAll(filepath.Join(root, "internal", "export"), ".go")...)
	web := readAll(filepath.Join(root, "internal", "web"), ".go")
	src := readAll(filepath.Join(root, "web", "src"), ".ts", ".tsx")
	for _, p := range Parity {
		field := fieldOf[p.Key]
		name := p.Key[strings.Index(p.Key, ".")+1:]
		if p.TUI && len(ui) > 0 && !regexp.MustCompile(`\.`+field+`\b`).Match(ui) {
			t.Errorf("%s: Parity says the terminal UI honours it but internal/ui never references %s", p.Key, field)
		}
		if (!p.TUI || !p.Web) && p.Why == "" {
			t.Errorf("%s: a key one UI ignores needs a one-line Why in config.Parity", p.Key)
		}
		if p.Web && len(web)+len(src) > 0 {
			js := camel(name)
			// distinctive references only: a field selector in Go (ed.Wrap), a property access or a quoted
			// key in the client (.Wrap, cfg(meta, 'editor', 'closeBraces')), never a bare word that any comment or label could contain
			goRef := regexp.MustCompile(`\.` + field + `\b`)
			if secOf[p.Key] == "CSV" { // the csv table travels as one struct into the exporter and importer
				goRef = regexp.MustCompile(`\.(` + field + `|CSV)\b`)
			}
			tsRef := regexp.MustCompile(`\.(` + field + `|` + js + `)\b|['"` + "`" + `](` + field + `|` + js + `)['"` + "`" + `]`)
			if !goRef.Match(web) && !tsRef.Match(src) {
				t.Errorf("%s: Parity says the web UI honours it but neither internal/web (%s) nor web/src (%s) references it", p.Key, field, js)
			}
		}
	}
}

// TestParityNineKeysBothUIs: the keys M14 check 3 found terminal-only are honoured by both UIs.
func TestParityNineKeysBothUIs(t *testing.T) {
	want := []string{"table.filter_default_column", "table.filter_default_operator", "table.filter_state", "table.default_sort", "table.alternating_rows",
		"general.new_tab_opens", "general.estimate_count_over", "general.reopen_workspaces", "editor.highlight_current", "editor.close_braces",
		"editor.complete_key", "editor.autocomplete", "editor.auto_save_queries", "sidebar.matching"}
	have := map[string]ParityKey{}
	for _, p := range Parity {
		have[p.Key] = p
	}
	for _, k := range want {
		if p, ok := have[k]; !ok || !p.TUI || !p.Web {
			t.Errorf("%s must be listed in config.Parity with TUI and Web true (listed: %v)", k, ok)
		}
	}
}
