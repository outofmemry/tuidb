package web

import (
	"net/http"
	"os"
	"strings"
	"webtuidb/internal/board"
	"webtuidb/internal/db"
)

func boardJSON(b board.Board) map[string]any {
	e := ""
	if b.Err != nil {
		e = b.Err.Error()
	}
	return map[string]any{"name": b.Name, "refreshSecs": b.RefreshSecs, "connection": b.Connection, "vars": nonNil(b.Vars), "panels": nonNil(b.Panels), "error": e}
}

func nonNil[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}

func (s *Server) boards(r *http.Request) (any, error) {
	l, err := board.List()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, b := range l {
		out = append(out, boardJSON(b))
	}
	return out, nil
}

func findBoard(name string) (board.Board, error) {
	l, err := board.List()
	if err != nil {
		return board.Board{}, err
	}
	for _, b := range l {
		if b.Name == name && b.Err == nil {
			return b, nil
		}
	}
	return board.Board{}, bad("no board named %q", name)
}

// boardSave adds a chart to the board of that name (creating it), so an existing board is never overwritten.
func (s *Server) boardSave(r *http.Request) (any, error) {
	var req struct {
		Name, Query string
		Kind, X, Y  string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Query) == "" {
		return nil, bad("a board name and a query are required")
	}
	spec, _ := db.ParseChart(req.Query) // a "-- barchart x: a, y: b" comment names the columns
	for _, f := range []struct {
		to *string
		v  string
	}{{&spec.Kind, req.Kind}, {&spec.X, req.X}, {&spec.Y, req.Y}} {
		if f.v != "" {
			*f.to = f.v
		}
	}
	if req.Y != "" { // an explicit y replaces the comment's series; FromChart prefers Ys
		spec.Ys = []string{req.Y}
	}
	nb := board.FromChart(req.Name, req.Query, spec)
	if old, err := findBoard(req.Name); err == nil {
		old.Panels = append(old.Panels, nb.Panels...)
		nb = old
	}
	if err := nb.Save(); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}

func (s *Server) boardRun(ss *session, r *http.Request) (any, error) {
	var req struct {
		Name string
		Vals map[string]string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	dialect := ss.o.Sess.Caps().Dialect
	for _, p := range b.Panels { // a board file must not become a write path around safe mode
		if !panelRuns(p) {
			continue
		}
		if !db.IsReadOnly(dialect, p.Query) {
			return nil, bad("panel %q is not read-only; boards only run read-only queries", p.Title)
		}
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	out := []panelOut{}
	for _, pr := range board.Run(ctx, ss.o.Sess, b, req.Vals, ss.rowLimit()) {
		out = append(out, toPanelOut(pr))
	}
	return map[string]any{"panels": out}, nil
}

// boardDuplicate copies a board under a new name (the panels and variables; a board is never overwritten).
func (s *Server) boardDuplicate(r *http.Request) (any, error) {
	var req struct{ Name, NewName string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.NewName) == "" {
		return nil, bad("a name for the copy is required")
	}
	b, err := findBoard(req.Name)
	if err != nil {
		return nil, err
	}
	b.Name, b.Path = strings.TrimSpace(req.NewName), ""
	if err := b.Save(); err != nil {
		return nil, bad("%v", err)
	}
	return nil, nil
}

func boardNamed(name string) (board.Board, bool) {
	l, _ := board.List()
	for _, b := range l {
		if b.Name == name {
			return b, true
		}
	}
	return board.Board{}, false
}

// boardPut saves the whole board under {name}: a new name is a new file, the same name rewrites it, and a different Name
// renames it (saved under the new name, then the old file is removed).
func (s *Server) boardPut(r *http.Request) (any, error) {
	var req struct {
		Name        string
		RefreshSecs int
		Connection  string
		Vars        []board.Var
		Panels      []board.Panel
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Panels) == 0 {
		return nil, bad("a board needs a name and at least one panel")
	}
	titles := map[string]board.Panel{}
	for _, p := range req.Panels {
		if strings.TrimSpace(p.Title) == "" || titles[p.Title].Title != "" {
			return nil, bad("every panel needs its own title")
		}
		titles[p.Title] = p
	}
	for _, p := range req.Panels {
		switch p.Kind {
		case "number", "table", "bar", "line":
			if strings.TrimSpace(p.Query) == "" {
				return nil, bad("panel %q needs a query", p.Title)
			}
		case "button":
			if t, ok := titles[p.Target]; !ok || !panelRuns(t) {
				return nil, bad("button %q must target a panel that runs a query", p.Title)
			}
		case "import":
			if strings.TrimSpace(p.Table) == "" {
				return nil, bad("import panel %q needs a table", p.Title)
			}
		case "input":
			if strings.TrimSpace(p.Var) == "" {
				return nil, bad("input panel %q needs the variable it sets", p.Title)
			}
		case "dropdown":
			if strings.TrimSpace(p.Var) == "" || (len(p.Options) == 0 && strings.TrimSpace(p.Query) == "") {
				return nil, bad("dropdown panel %q needs a variable and either options or a query", p.Title)
			}
		case "exporter":
			if strings.TrimSpace(p.Query) == "" {
				return nil, bad("exporter panel %q needs a query", p.Title)
			}
			switch p.Format {
			case "csv", "json", "sql", "excel":
			default:
				return nil, bad("exporter panel %q: format must be csv, json, sql or excel", p.Title)
			}
		default:
			return nil, bad("panel %q: kind must be number, table, bar, line, button, import, input, dropdown or exporter", p.Title)
		}
		if p.RefreshOn != "" && titles[p.RefreshOn].Title == "" {
			return nil, bad("panel %q refreshes on %q, which is not a panel", p.Title, p.RefreshOn)
		}
	}
	nb := board.Board{Name: req.Name, RefreshSecs: req.RefreshSecs, Connection: req.Connection, Vars: req.Vars, Panels: req.Panels}
	old, had := boardNamed(r.PathValue("name"))
	if had && req.Connection == "" { // the pin (set in the terminal or earlier) survives a save that does not name one
		nb.Connection = old.Connection
	}
	if had && req.Name == old.Name {
		nb.Path = old.Path
	} else if _, clash := boardNamed(req.Name); clash {
		return nil, bad("a board named %q already exists", req.Name)
	}
	if err := nb.Save(); err != nil {
		return nil, bad("%v", err)
	}
	if had && req.Name != old.Name {
		os.Remove(old.Path)
	}
	return nil, nil
}

func (s *Server) boardDelete(r *http.Request) (any, error) {
	b, ok := boardNamed(r.PathValue("name"))
	if !ok {
		return nil, bad("no board named %q", r.PathValue("name"))
	}
	return nil, os.Remove(b.Path)
}
