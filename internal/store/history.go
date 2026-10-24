// Package store persists query history, saved queries and UI state under config.DataDir().
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"tuidb/internal/config"
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

// History is one JSONL file per connection, kept to the newest maxEntries.
type History struct {
	Dir string
	mu  sync.Mutex
}

const maxEntries = 5000

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
	unlock, err := config.LockFile(h.path(conn)) // trim/clear in another process must not orphan this write
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(h.path(conn), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if st, err := f.Stat(); err == nil && st.Size() > 0 { // a torn last line must not absorb this entry
		var last [1]byte
		if _, err := f.ReadAt(last[:], st.Size()-1); err == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	n, werr := f.Write(line)
	f.Close()
	if werr != nil {
		return werr
	}
	// The file only ever grows, so trim it now and then: keep the newest maxEntries.
	if st, err := os.Stat(h.path(conn)); err == nil && st.Size() > int64(n)*maxEntries*2 && st.Size() > 1<<20 {
		all, err := h.readAll(conn)
		if err == nil && len(all) > maxEntries {
			drop := len(all) - maxEntries
			return h.rewrite(conn, func(e HistoryEntry) bool { drop--; return drop < 0 })
		}
	}
	return nil
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range all {
		if keep(e) {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
	}
	return config.WriteFileAtomic(h.path(conn), buf.Bytes())
}

func (h *History) Delete(conn, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	unlock, err := config.LockFile(h.path(conn))
	if err != nil {
		return err
	}
	defer unlock()
	return h.rewrite(conn, func(e HistoryEntry) bool { return e.ID != id })
}

func (h *History) Clear(conn string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return err
	}
	unlock, err := config.LockFile(h.path(conn))
	if err != nil {
		return err
	}
	defer unlock()
	err = os.Remove(h.path(conn))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
