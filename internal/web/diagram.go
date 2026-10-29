package web

import (
	"net/http"
	"webtuidb/internal/db"
	"webtuidb/internal/diagram"
)

func (s *Server) diagram(ss *session, r *http.Request) (any, error) {
	ctx, cancel := ss.ctx(r)
	defer cancel()
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = ss.o.Sess.CurrentSchema()
	}
	tabs, err := diagram.Load(ctx, ss.o.Sess, schema)
	if err != nil {
		return nil, bad("%v", err)
	}
	cl := diagram.Cluster(tabs)
	type fkJSON struct {
		Columns    []string `json:"columns"`
		RefSchema  string   `json:"refSchema"`
		RefTable   string   `json:"refTable"`
		RefColumns []string `json:"refColumns"`
	}
	type tabJSON struct {
		Ref     db.ObjRef `json:"ref"`
		Columns []colJSON `json:"columns"`
		FKs     []fkJSON  `json:"fks"`
		Cluster int       `json:"cluster"`
	}
	out := make([]tabJSON, len(tabs))
	for i, t := range tabs {
		o := tabJSON{Ref: t.Ref, Columns: []colJSON{}, FKs: []fkJSON{}, Cluster: cl[t.Ref.Schema+"."+t.Ref.Name]}
		for _, c := range t.Columns {
			o.Columns = append(o.Columns, colJSON{Name: c.Name, Type: c.Type, PK: c.PK})
		}
		for _, f := range t.FKs {
			o.FKs = append(o.FKs, fkJSON{f.Columns, f.RefSchema, f.RefTable, f.RefColumns})
		}
		out[i] = o
	}
	schemas, err := diagram.Schemas(ctx, ss.o.Sess)
	if err != nil {
		return nil, bad("%v", err)
	}
	return map[string]any{"schema": schema, "schemas": nonNil(schemas), "tables": out}, nil
}
