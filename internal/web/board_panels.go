package web

import (
	"context"
	"net/http"
	"os"
	"strings"
	"webtuidb/internal/board"
	"webtuidb/internal/db"
	"webtuidb/internal/export"
)

func boardPanelNamed(b board.Board, title string) (board.Panel, bool) {
	for _, p := range b.Panels {
		if p.Title == title {
			return p, true
		}
	}
	return board.Panel{}, false
}

func (s *Server) boardOptions(ss *session, r *http.Request) (any, error) {
	b, err := findBoard(r.PathValue("name"))
	if err != nil {
		return nil, err
	}
	p, ok := boardPanelNamed(b, r.URL.Query().Get("panel"))
	if !ok || p.Kind != "dropdown" {
		return nil, bad("no dropdown panel %q in %q", r.URL.Query().Get("panel"), b.Name)
	}
	if len(p.Options) == 0 && strings.TrimSpace(p.Query) != "" && !db.IsReadOnly(ss.o.Sess.Caps().Dialect, p.Query) {
		return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	opts, cut, err := board.DropdownOptions(ctx, ss.o.Sess, b, p, nil)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"options": nonNil(opts), "truncated": cut}, nil
}

// boardExport runs an exporter panel's query as a background export job (download it like any other export).
func (s *Server) boardExport(ss *session, r *http.Request) (any, error) {
	var req struct {
		Panel, Format string
		Vals          map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(r.PathValue("name"))
	if err != nil {
		return nil, err
	}
	p, ok := boardPanelNamed(b, req.Panel)
	if !ok || strings.TrimSpace(p.Query) == "" {
		return nil, bad("no exporter panel %q in %q", req.Panel, b.Name)
	}
	format := req.Format
	if format == "" {
		format = p.Format
	}
	if format == "excel" {
		format = "xlsx"
	}
	switch format {
	case "csv", "json", "sql", "xlsx":
	default:
		return nil, bad("unknown export format %q", format)
	}
	sess := ss.o.Sess
	if !db.IsReadOnly(sess.Caps().Dialect, p.Query) {
		return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	if err := ss.reserve(0); err != nil {
		return nil, err
	}
	o := export.Options{Format: export.Format(format), CSV: s.o.Cfg.CSV, Gzip: false, Table: p.Title, Dialect: sess.Caps().Dialect, NoHeader: !s.o.Cfg.Export.Header}
	f, err := os.CreateTemp("", "webtuidb-export-*")
	if err != nil {
		return nil, err
	}
	j := &job{kind: "export", title: "Export " + p.Title, file: f.Name(), fileName: ss.exportName("", b.Name+"-"+p.Title, export.Ext(o))}
	id := ss.startJob(j, func(ctx context.Context) error {
		res, err := board.ExportPanel(ctx, sess, b, p, req.Vals, ss.rowLimit()) // shortcut: capped at the row limit, not paged
		if err == nil {
			err = export.Write(f, res, o)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			j.mu.Lock()
			j.file = ""
			j.mu.Unlock()
		}
		return err
	})
	return map[string]any{"job": id}, nil
}

type namedSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

func jsonSeries(l []db.NamedSeries) []namedSeries {
	out := []namedSeries{}
	for _, s := range l {
		out = append(out, namedSeries{s.Name, nonNil(s.Values)})
	}
	return out
}

type panelOut struct {
	Title  string        `json:"title"`
	Kind   string        `json:"kind"`
	Number string        `json:"number,omitempty"`
	Error  string        `json:"error,omitempty"`
	Result *resultJSON   `json:"result,omitempty"`
	Labels []string      `json:"labels"`
	Values []float64     `json:"values"`
	Series []namedSeries `json:"series"`
}

func toPanelOut(pr board.PanelResult) panelOut {
	p := panelOut{Title: pr.Panel.Title, Kind: pr.Panel.Kind, Number: pr.Number, Labels: nonNil(pr.Series.Labels), Values: nonNil(pr.Series.Values), Series: jsonSeries(pr.Series.Series)}
	if pr.Err != nil {
		p.Error = pr.Err.Error()
	}
	if pr.Result != nil && pr.Panel.Kind == "table" {
		rj := toJSON(pr.Result, nil)
		p.Result = &rj
	}
	return p
}

// panelRuns: button, import, input, dropdown and exporter panels are not run by board.Run; the UIs act on them.
func panelRuns(p board.Panel) bool {
	switch p.Kind {
	case "button", "import", "input", "dropdown", "exporter":
		return false
	}
	return true
}

// boardPanel runs the one panel titled `title` ({name, title, vals}) and names the panels that refresh when it runs.
func (s *Server) boardPanel(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name, Title string
		Vals        map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	for _, p := range b.Panels {
		if p.Title != req.Title {
			continue
		}
		if !panelRuns(p) {
			return nil, bad("panel %q is a %s panel: the UI acts on it", p.Title, p.Kind)
		}
		if !db.IsReadOnly(ss.o.Sess.Caps().Dialect, p.Query) {
			return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
		}
		ctx, cancel := ss.ctx(r)
		defer cancel()
		deps := []string{}
		for _, d := range board.Dependents(b, p.Title) {
			deps = append(deps, d.Title)
		}
		return map[string]any{"panel": toPanelOut(board.RunPanel(ctx, ss.o.Sess, b, p, req.Vals, ss.rowLimit())), "dependents": deps}, nil
	}
	return nil, bad("no panel titled %q in %q", req.Title, req.Name)
}
