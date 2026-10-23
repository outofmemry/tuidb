// Package board is the metrics board: TOML files of parameterised query panels.
package board

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

type Var struct {
	Name, Label, Default string
	Options              []string
}

type Panel struct {
	Title, Kind, Query, Chart string
	Target                    string `toml:"target,omitempty"`     // kind "button": the panel title it runs when clicked
	RefreshOn                 string `toml:"refresh_on,omitempty"` // re-run this panel when the named panel runs or its button is clicked
	Table                     string `toml:"table,omitempty"`      // kind "import": the import wizard's target table
}

type Board struct {
	Name        string
	RefreshSecs int     `toml:"refresh_secs"`
	Connection  string  `toml:"connection"` // run on this connection instead of the one the board is opened from; "" = the current one
	Vars        []Var   `toml:"var"`
	Panels      []Panel `toml:"panel"`
	Path        string  `toml:"-"`
	Err         error   `toml:"-"`
}

type PanelResult struct {
	Panel  Panel
	Result *db.Result
	Series db.ChartSeries
	Number string
	Err    error
}

func Dir() string { return filepath.Join(config.DataDir(), "boards") }

func Load(path string) (Board, error) {
	b := Board{Path: path}
	if _, err := toml.DecodeFile(path, &b); err != nil {
		return Board{}, err
	}
	return b, nil
}

// List parses every *.toml in Dir; a bad file becomes a Board named after the file with Err set.
func List() ([]Board, error) {
	files, err := filepath.Glob(filepath.Join(Dir(), "*.toml"))
	if err != nil {
		return nil, err
	}
	var out []Board
	for _, f := range files {
		b, err := Load(f)
		if err != nil {
			b = Board{Name: filepath.Base(f), Path: f, Err: err}
		}
		out = append(out, b)
	}
	return out, nil
}

// Save writes b to b.Path, or <Dir>/<name>.toml when Path is empty. It works on a copy; use b.Save to
// have the chosen Path recorded in the caller's board.
func Save(b Board) error { return b.Save() }

// Save writes b to b.Path, or <Dir>/<name>.toml when Path is empty (and records it in b).
func (b *Board) Save() error {
	if rel, err := filepath.Rel(Dir(), b.Path); b.Path != "" && (err != nil || strings.HasPrefix(rel, "..")) {
		b.Path = "" // a Path outside the boards dir is not ours to write
	}
	if b.Path == "" {
		n := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == 0 {
				return '_'
			}
			return r
		}, b.Name)
		if n == "" || n == "." || n == ".." {
			return fmt.Errorf("board needs a usable name")
		}
		b.Path = filepath.Join(Dir(), n+".toml")
		if _, err := os.Stat(b.Path); err == nil {
			return fmt.Errorf("board %q already exists", n)
		}
	}
	if err := os.MkdirAll(filepath.Dir(b.Path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(b); err != nil {
		return err
	}
	return config.WriteFileAtomic(b.Path, buf.Bytes())
}

// Run executes every panel with vars bound; one panel failing does not stop the others.
func Run(ctx context.Context, sess db.Session, b Board, vals map[string]string, limit int) []PanelResult {
	known := bindVars(b, vals)
	dialect := sess.Caps().Dialect
	out := make([]PanelResult, 0, len(b.Panels))
	for _, p := range b.Panels {
		if p.Kind == "button" || p.Kind == "import" { // the UIs handle these
			continue
		}
		out = append(out, runPanel(ctx, sess, p, known, dialect, limit))
	}
	return out
}

func bindVars(b Board, vals map[string]string) map[string]string {
	known := map[string]string{}
	for _, v := range b.Vars {
		known[v.Name] = v.Default
	}
	for k, v := range vals {
		if _, ok := known[k]; ok { // only declared vars bind, so a stray key cannot rewrite a query
			known[k] = v
		}
	}
	return known
}

func runPanel(ctx context.Context, sess db.Session, p Panel, known map[string]string, dialect string, limit int) PanelResult {
	r := PanelResult{Panel: p}
	r.run(ctx, sess, known, dialect, limit)
	return r
}

// RunPanel runs one panel of b with vals bound like Run does.
func RunPanel(ctx context.Context, sess db.Session, b Board, p Panel, vals map[string]string, limit int) PanelResult {
	return runPanel(ctx, sess, p, bindVars(b, vals), sess.Caps().Dialect, limit)
}

// Dependents returns the panels that refresh when the panel titled title runs.
func Dependents(b Board, title string) []Panel {
	var out []Panel
	for _, p := range b.Panels {
		if p.RefreshOn == title {
			out = append(out, p)
		}
	}
	return out
}

func (r *PanelResult) run(ctx context.Context, sess db.Session, vals map[string]string, dialect string, limit int) {
	if !db.IsReadOnly(dialect, r.Panel.Query) {
		r.Err = fmt.Errorf("panels must be read-only")
		return
	}
	q := r.Panel.Query
	if dialect == "mongo" { // $match, $group... are operators there, so nothing is bound; refuse rather than run a literal "$var"
		for n := range vals {
			if regexp.MustCompile(`\$` + regexp.QuoteMeta(n) + `\b`).MatchString(q) {
				r.Err = fmt.Errorf("variable $%s cannot be bound in a mongo panel", n)
				return
			}
		}
	} else {
		for _, n := range db.Params(q, "dollar", dialect) {
			if _, ok := vals[n]; !ok {
				r.Err = fmt.Errorf("unknown variable $%s", n)
				return
			}
		}
		q = db.Bind(q, "dollar", vals, dialect)
	}
	res, err := sess.Exec(ctx, q, limit)
	if err != nil {
		r.Err = err
		return
	}
	r.Result = res
	switch r.Panel.Kind {
	case "number":
		if len(res.Rows) > 0 && len(res.Rows[0]) > 0 {
			r.Number = fmt.Sprint(res.Rows[0][0])
		}
	case "bar", "line":
		spec, _ := db.ParseChart("-- " + r.Panel.Kind + "chart " + r.Panel.Chart)
		r.Series, r.Err = db.Chart(res, spec)
	}
}

// FromChart builds a one-panel board from a query and chart spec.
func FromChart(name, query string, spec db.ChartSpec) Board {
	chart := ""
	if spec.X != "" {
		chart = "x: " + spec.X
	}
	ys := spec.Ys
	if len(ys) == 0 && spec.Y != "" {
		ys = []string{spec.Y}
	}
	if len(ys) > 0 {
		if chart != "" {
			chart += ", "
		}
		chart += "y: " + strings.Join(ys, ", ")
	}
	kind := spec.Kind
	if kind != "bar" && kind != "line" {
		kind = "bar" // shortcut: pie charts have no board panel yet, shown as bar
	}
	return Board{Name: name, RefreshSecs: 30, Panels: []Panel{{Title: name, Kind: kind, Query: query, Chart: chart}}}
}
