// Package store persists query history, saved queries and UI state under config.DataDir().
package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"webtuidb/internal/config"
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

const (
	maxEntries  = 5000
	maxSQLBytes = 64 << 10 // longer statements are cut when logged
	maxFileSize = 16 << 20 // the file is trimmed to half of this once it grows past it
)

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// path names the connection's file: the name itself when it is plain, else percent-encoded so "a b" and "a_b" differ.
func (h *History) path(conn string) string {
	if !unsafeName.MatchString(conn) {
		return filepath.Join(h.Dir, conn+".jsonl")
	}
	var b strings.Builder
	for i := 0; i < len(conn); i++ {
		if c := conn[i]; c != '%' && !unsafeName.MatchString(string(c)) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return filepath.Join(h.Dir, b.String()+".jsonl")
}

// MigrateLegacy adopts history files written before the percent-encoding ("a b" was stored as "a_b.jsonl"),
// once, at startup. A legacy name that another live connection owns (or that two connections would share)
// is left alone.
func (h *History) MigrateLegacy(connNames []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	owners := map[string]int{} // legacy file name -> number of live connections that map to it
	for _, n := range connNames {
		owners[unsafeName.ReplaceAllString(n, "_")+".jsonl"]++
		owners[filepath.Base(h.path(n))]++ // a plain-named connection owns its own file
	}
	for _, n := range connNames {
		if !unsafeName.MatchString(n) {
			continue
		}
		legacy := unsafeName.ReplaceAllString(n, "_") + ".jsonl"
		if owners[legacy] != 1 {
			continue
		}
		p := h.path(n)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		os.Rename(filepath.Join(h.Dir, legacy), p)
	}
}

// entryID is the one ID scheme for every entry: a hash of the connection and everything that identifies the
// statement, taken before the SQL is cut to maxSQLBytes so long statements sharing a prefix stay distinct.
func entryID(conn string, e HistoryEntry) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{conn, e.Database, e.SQL, e.Original,
		e.Time.UTC().Format(time.RFC3339Nano), e.Duration.String()}, "\x00")))
	return hex.EncodeToString(sum[:12])
}

// clip cuts the logged text to maxSQLBytes.
func clip(e *HistoryEntry) {
	if len(e.SQL) > maxSQLBytes {
		e.SQL = strings.ToValidUTF8(e.SQL[:maxSQLBytes], "") + "\n-- (truncated)"
	}
	if len(e.Original) > maxSQLBytes {
		e.Original = strings.ToValidUTF8(e.Original[:maxSQLBytes], "")
	}
}

func (h *History) Append(conn string, e HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.ID = entryID(conn, e)
	clip(&e)
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
	return h.trimLocked(conn, n)
}

// AppendAll writes entries in one lock/open/write and reports how many were new. An entry keeps its ID when
// it has one (an exported history file does), otherwise it gets the same ID Append would give it. Entries
// already present are skipped, matched by ID or by their content (database, SQL, original, time, duration),
// so importing the same file twice, or a file written before IDs were unified, adds nothing. The batch is
// stored oldest first whatever order it came in, so the trim keeps the newest entries; when it is older than
// what the file already holds the file is rewritten in time order. Any batch size works.
func (h *History) AppendAll(conn string, entries []HistoryEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(h.Dir, 0o700); err != nil {
		return 0, err
	}
	unlock, err := config.LockFile(h.path(conn))
	if err != nil {
		return 0, err
	}
	defer unlock()
	existing, err := h.readAll(conn)
	if err != nil {
		return 0, err
	}
	have := make(map[string]bool, len(existing)*2)
	var newest time.Time
	for _, e := range existing {
		have[e.ID] = true
		have[entryID(conn, e)] = true
		if e.Time.After(newest) {
			newest = e.Time
		}
	}
	now := time.Now()
	var fresh []HistoryEntry
	for _, e := range entries {
		if e.Time.IsZero() {
			e.Time = now
		}
		key := entryID(conn, e)
		if e.ID == "" {
			e.ID = key
		}
		// Stored entries are matched by their clipped text, so also try the key of the clipped form.
		c := e
		clip(&c)
		ckey := entryID(conn, c)
		if have[e.ID] || have[key] || have[ckey] {
			continue
		}
		have[e.ID], have[key] = true, true // ckey stays out: two long statements differing past the clip are distinct
		clip(&e)
		fresh = append(fresh, e)
	}
	if len(fresh) == 0 {
		return 0, nil
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].Time.Before(fresh[j].Time) })
	if fresh[0].Time.Before(newest) { // interleaves with what is there: keep the whole file in time order
		all := append(existing, fresh...)
		sort.SliceStable(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for _, e := range all {
			if err := enc.Encode(e); err != nil {
				return 0, err
			}
		}
		if err := config.WriteFileAtomic(h.path(conn), buf.Bytes()); err != nil {
			return 0, err
		}
		return len(fresh), h.trimLocked(conn, buf.Len()/len(all)+1)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range fresh {
		if err := enc.Encode(e); err != nil {
			return 0, err
		}
	}
	f, err := os.OpenFile(h.path(conn), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	out := buf.Bytes()
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], st.Size()-1); err == nil && last[0] != '\n' {
			out = append([]byte{'\n'}, out...)
		}
	}
	_, werr := f.Write(out)
	f.Close()
	if werr != nil {
		return 0, werr
	}
	return len(fresh), h.trimLocked(conn, buf.Len()/len(fresh)+1)
}

// trimLocked keeps the file to the newest maxEntries; n is the size of one just-written line.
func (h *History) trimLocked(conn string, n int) error {
	// The file only ever grows, so trim it now and then: keep the newest maxEntries.
	if st, err := os.Stat(h.path(conn)); err == nil && (st.Size() > maxFileSize || st.Size() > int64(n)*maxEntries*2 && st.Size() > 1<<20) {
		all, err := h.readAll(conn)
		if err != nil {
			return nil
		}
		// keep the newest entries: at most maxEntries, and at most half the byte cap
		keep, size := 0, 0
		for i := len(all) - 1; i >= 0 && keep < maxEntries; i-- {
			size += len(all[i].SQL) + len(all[i].Original) + 128
			if size > maxFileSize/2 && keep > 0 {
				break
			}
			keep++
		}
		if drop := len(all) - keep; drop > 0 {
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
