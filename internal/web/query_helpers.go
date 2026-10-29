package web

import (
	"net/http"
	"webtuidb/internal/db"
	"webtuidb/internal/sqlfmt"
)

func (s *Server) format(r *http.Request) (any, error) {
	var req struct{ Text, Mode, Dialect, IndentType string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	switch req.Mode {
	case "beautify":
		e := s.o.Cfg.Editor
		if req.IndentType == "" {
			req.IndentType = e.IndentType
		}
		return map[string]string{"text": sqlfmt.Beautify(req.Text, sqlfmt.Options{Indent: e.Indent, Width: e.IndentWidth, Uppercase: e.UppercaseKeywords, Dialect: req.Dialect, IndentType: req.IndentType})}, nil
	case "uglify":
		return map[string]string{"text": sqlfmt.Uglify(req.Text, req.Dialect)}, nil
	}
	return nil, bad("mode must be beautify or uglify")
}

// paramNames lists the distinct parameter names across statements, in order of appearance.
func paramNames(stmts []db.Stmt, pf db.ParamFormat, dialect string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, st := range stmts {
		for _, n := range db.Params(st.Text, pf, dialect) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

func (s *Server) params(r *http.Request) (any, error) {
	var req struct {
		Text, Dialect, ParamFormat string
		Current                    bool // only the statement at Offset (a byte offset), like /exec
		Offset                     int
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	pf := db.ParamFormat(s.o.Cfg.Editor.ParamFormat)
	if req.ParamFormat != "" {
		pf = db.ParamFormat(req.ParamFormat)
	}
	stmts := []db.Stmt{{Text: req.Text}}
	if req.Current {
		st, ok := db.StatementAt(db.Split(req.Dialect, req.Text), req.Offset)
		if !ok {
			return nil, bad("no statement at the cursor")
		}
		stmts = []db.Stmt{st}
	}
	return map[string]any{"params": paramNames(stmts, pf, req.Dialect)}, nil
}

// chart draws a result the browser already holds: cells arrive as the strings the grid shows.
func (s *Server) chart(r *http.Request) (any, error) {
	var req struct {
		Stmt, Kind string
		Columns    []string
		Cells      [][]*string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	res := &db.Result{}
	for _, c := range req.Columns {
		res.Columns = append(res.Columns, db.ResultColumn{Name: c})
	}
	for _, row := range req.Cells {
		if len(row) != len(req.Columns) {
			return nil, bad("every row needs %d cells", len(req.Columns))
		}
		out := make([]any, len(row))
		for i, c := range row {
			if c != nil {
				out[i] = *c
			}
		}
		res.Rows = append(res.Rows, out)
	}
	spec, _ := db.ParseChart(req.Stmt) // no "-- barchart" comment: infer the columns
	if spec.Kind == "" {
		spec.Kind = "bar"
	}
	if req.Kind != "" {
		spec.Kind = req.Kind
	}
	ser, err := db.Chart(res, spec)
	if err != nil {
		return nil, bad("%v", err)
	}
	if ser.Labels == nil {
		ser.Labels, ser.Values = []string{}, []float64{}
	}
	return map[string]any{"kind": spec.Kind, "labels": ser.Labels, "values": ser.Values, "series": jsonSeries(ser.Series)}, nil
}
