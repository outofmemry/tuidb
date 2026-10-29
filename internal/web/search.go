package web

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
	"webtuidb/internal/db"
)

type hitJSON struct {
	Ref     db.ObjRef    `json:"ref"`
	Count   int64        `json:"count"`
	Filters db.FilterSet `json:"filters"`
	Error   string       `json:"error,omitempty"`
	Schema  string       `json:"schema"`
}

func (s *Server) searchStart(ss *session, r *http.Request) (any, error) {
	var in struct {
		db.SearchOptions
		AllSchemasSnake bool `json:"all_schemas"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	req := in.SearchOptions
	req.AllSchemas = req.AllSchemas || in.AllSchemasSnake
	switch req.Op {
	case "Contains", "Not contains", "=", "Has prefix", "Has suffix":
	default:
		return nil, bad("unknown operator %q", req.Op)
	}
	if req.Value == "" {
		return nil, bad("a value is required")
	}
	ctx, cancel := ss.ctx(r)
	defer cancel()
	schemas, err := db.SearchSchemas(ctx, ss.o.Sess, req)
	if err != nil {
		return nil, bad("%v", err)
	}
	var objs []db.Object
	for _, schema := range schemas {
		if schema == "" {
			schema = ss.o.Sess.CurrentSchema()
		}
		one, err := ss.o.Sess.Objects(ctx, schema)
		if err != nil {
			return nil, bad("%v", err)
		}
		objs = append(objs, one...)
	}
	only := map[string]bool{}
	for _, t := range req.Tables {
		only[t] = true
	}
	if err := ss.jobSlot(); err != nil {
		return nil, err
	}
	j := &job{kind: "search", title: fmt.Sprintf("Search for %q", req.Value), paused: new(atomic.Bool)}
	for _, ob := range objs {
		if ob.Kind == db.KindTable && (len(only) == 0 || only[ob.Name]) {
			j.total++
		}
	}
	id := ss.startJob(j, func(ctx context.Context) error {
		return db.Search(ctx, ss.o.Sess, req, func(h db.SearchHit) {
			j.record(h)
			if !waitWhilePaused(ctx, j.paused, maxPause) { // between two tables, outside the lock
				j.cancel()
			}
		})
	})
	return map[string]any{"job": id, "total": j.total}, nil
}

// searchPause / searchResume flip the flag the search job checks between tables.
func (s *Server) searchPause(ss *session, r *http.Request) (any, error) { return ss.setPaused(r, true) }

func (s *Server) searchResume(ss *session, r *http.Request) (any, error) {
	return ss.setPaused(r, false)
}

func (ss *session) setPaused(r *http.Request, v bool) (any, error) {
	ss.mu.Lock()
	j := ss.jobs[r.PathValue("job")]
	ss.mu.Unlock()
	if j == nil || j.paused == nil {
		return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such search"}
	}
	j.paused.Store(v)
	return map[string]any{"paused": v}, nil
}

// record notes one table's result under the job lock.
func (j *job) record(h db.SearchHit) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.checked++
	if h.Count > 0 || h.Err != nil {
		x := hitJSON{Ref: h.Ref, Count: h.Count, Filters: h.Filters, Schema: h.Ref.Schema}
		if h.Err != nil {
			x.Error = h.Err.Error()
		}
		j.hits = append(j.hits, x)
	}
}

// maxPause is how long a search may stay paused before it is cancelled and frees its job slot.
const maxPause = 10 * time.Minute

// waitWhilePaused blocks a search between two tables until it is resumed or cancelled, holding no lock.
// It returns false when the pause outlasted max (the caller then cancels the job).
func waitWhilePaused(ctx context.Context, p *atomic.Bool, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for p.Load() && ctx.Err() == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}
