package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"tuidb/internal/db"
)

// TableState is remembered per connection/database/table.
type TableState struct {
	ColWidths map[string]int `json:"widths,omitempty"`
	Hidden    []string       `json:"hidden,omitempty"`
	Filters   db.FilterSet   `json:"filters,omitempty"`
	ShowBar   bool           `json:"show_filters,omitempty"`
	Sort      []db.Sort      `json:"sort,omitempty"`
}

type Workspace struct {
	Connection string `json:"connection"`
	Database   string `json:"database"`
}

type stateFile struct {
	Tables   map[string]TableState `json:"tables,omitempty"`
	Pinned   map[string][]string   `json:"pinned,omitempty"` // conn -> "schema.name"
	Recent   map[string][]string   `json:"recent,omitempty"`
	Last     []Workspace           `json:"last_session,omitempty"`
	RowLimit map[string]int        `json:"row_limit,omitempty"` // conn -> editor row limit
}

// State is the JSON file with everything the UI remembers between runs.
type State struct {
	Path string
	mu   sync.Mutex
	d    stateFile
}

func LoadState(path string) (*State, error) {
	s := &State{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, err // refuse to overwrite a file we could not read
	}
	return s, nil
}

// do mutates under the lock and saves atomically.
func (s *State) do(f func(*stateFile)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.d)
	b, err := json.MarshalIndent(s.d, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func TableKey(conn, database, table string) string { return conn + "\x00" + database + "\x00" + table }

// Table returns a copy: callers edit filters in place, which must not leak into the saved state
// until SetTable is called.
func (s *State) Table(key string) TableState {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.d.Tables[key]
	t.Hidden = append([]string(nil), t.Hidden...)
	t.Sort = append([]db.Sort(nil), t.Sort...)
	t.Filters.Filters = append([]db.Filter(nil), t.Filters.Filters...)
	if t.ColWidths != nil {
		w := make(map[string]int, len(t.ColWidths))
		for k, v := range t.ColWidths {
			w[k] = v
		}
		t.ColWidths = w
	}
	return t
}

func (s *State) SetTable(key string, t TableState) error {
	return s.do(func(d *stateFile) {
		if d.Tables == nil {
			d.Tables = map[string]TableState{}
		}
		d.Tables[key] = t
	})
}

func (s *State) Pinned(conn string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.d.Pinned[conn]...)
}

// TogglePin pins or unpins an item and reports whether it is now pinned.
func (s *State) TogglePin(conn, item string) (pinned bool, err error) {
	err = s.do(func(d *stateFile) {
		if d.Pinned == nil {
			d.Pinned = map[string][]string{}
		}
		list := d.Pinned[conn]
		for i, it := range list {
			if it == item {
				d.Pinned[conn] = append(list[:i:i], list[i+1:]...)
				return
			}
		}
		d.Pinned[conn] = append(list, item)
		pinned = true
	})
	return
}

func (s *State) Recent(conn string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.d.Recent[conn]...)
}

// Touch moves item to the front of the recent list (max 10).
func (s *State) Touch(conn, item string) error {
	return s.do(func(d *stateFile) {
		if d.Recent == nil {
			d.Recent = map[string][]string{}
		}
		out := []string{item}
		for _, it := range d.Recent[conn] {
			if it != item && len(out) < 10 {
				out = append(out, it)
			}
		}
		d.Recent[conn] = out
	})
}

func (s *State) LastSession() []Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Workspace(nil), s.d.Last...)
}

func (s *State) SetLastSession(w []Workspace) error { return s.do(func(d *stateFile) { d.Last = w }) }

func (s *State) RowLimit(conn string, def int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.d.RowLimit[conn]; ok {
		return n
	}
	return def
}

func (s *State) SetRowLimit(conn string, n int) error {
	return s.do(func(d *stateFile) {
		if d.RowLimit == nil {
			d.RowLimit = map[string]int{}
		}
		d.RowLimit[conn] = n
	})
}
