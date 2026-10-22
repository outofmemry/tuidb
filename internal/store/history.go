// Package store persists query history, saved queries and UI state under config.DataDir().
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type HistoryEntry struct {
	ID       string        `json:"id"`
	Time     time.Time     `json:"time"`
	Database string        `json:"db,omitempty"`
	SQL      string        `json:"sql"`
	Duration time.Duration `json:"dur,omitempty"`
	Source   string        `json:"src"`            // "editor" | "gui"
	Original string        `json:"orig,omitempty"` // CSV pre-image when log_original_values is on
}

// History is one JSONL file per connection.
type History struct {
	Dir string
	mu  sync.Mutex
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (h *History) path(conn string) string {
	return filepath.Join(h.Dir, unsafeName.ReplaceAllString(conn, "_")+".jsonl")
}

func (h *History) Append(conn string, e HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.ID = fmt.Sprintf("%d", e.Time.UnixNano())
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path(conn), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (h *History) readAll(conn string) ([]HistoryEntry, error) {
	f, err := os.Open(h.path(conn))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []HistoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil { // a torn last line must not hide the rest
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// List returns entries newest first whose SQL contains search (case-insensitive). limit<=0 = all.
func (h *History) List(conn, search string, limit int) ([]HistoryEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	all, err := h.readAll(conn)
	if err != nil {
		return nil, err
	}
	search = strings.ToLower(search)
	var out []HistoryEntry
	for i := len(all) - 1; i >= 0; i-- {
		if search == "" || strings.Contains(strings.ToLower(all[i].SQL), search) {
			out = append(out, all[i])
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (h *History) rewrite(conn string, keep func(HistoryEntry) bool) error {
	all, err := h.readAll(conn)
	if err != nil {
		return err
	}
	tmp := h.path(conn) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, e := range all {
		if keep(e) {
			if err := enc.Encode(e); err != nil {
				f.Close()
				return err
			}
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, h.path(conn))
}

func (h *History) Delete(conn, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rewrite(conn, func(e HistoryEntry) bool { return e.ID != id })
}

func (h *History) Clear(conn string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := os.Remove(h.path(conn))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
