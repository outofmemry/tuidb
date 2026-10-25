package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
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
	Tables   map[string]TableState        `json:"tables,omitempty"`
	Pinned   map[string][]string          `json:"pinned,omitempty"` // conn -> "schema.name"
	Recent   map[string][]string          `json:"recent,omitempty"`
	Last     []Workspace                  `json:"last_session,omitempty"`
	RowLimit map[string]int               `json:"row_limit,omitempty"`      // conn -> editor row limit
	Folds    map[string][]int             `json:"folds,omitempty"`          // editor key (saved query path or conn/tab) -> folded statement indexes
	Tabs     map[string][]OpenTab         `json:"open_tabs,omitempty"`      // conn -> tabs restored on reconnect ([editor] auto_save_tabs)
	Backup   map[string]map[string]string `json:"backup_options,omitempty"` // conn -> last backup dialog options
	Files    []string                     `json:"recent_files,omitempty"`
	Sort     string                       `json:"connection_sort,omitempty"`
	Groups   map[string][]ItemGroup       `json:"groups,omitempty"` // conn -> custom sidebar groups
}

// OpenTab is one tab to restore: Kind "sql" (Text is the editor content) or "table" (Name is the object).
type OpenTab struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
	// SavedAs is the saved query (its path) a "sql" tab edits, so auto-save keeps writing to it after a restore.
	SavedAs string `json:"saved_as,omitempty"`
}

// State is the JSON file with everything the UI remembers between runs.
type State struct {
	Path      string
	Recovered string // set when a corrupt file was moved aside at load
	mu        sync.Mutex
	d         stateFile
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
		// Keep the unreadable file for inspection and start fresh instead of refusing to launch.
		bad := path + ".corrupt"
		if renameErr := os.Rename(path, bad); renameErr != nil {
			return nil, fmt.Errorf("%s is not valid JSON (%v) and could not be moved aside: %w", path, err, renameErr)
		}
		s.d = stateFile{}
		s.Recovered = fmt.Sprintf("%s was not valid JSON and was moved to %s", path, bad)
	}
	return s, nil
}

// do mutates under the lock and saves atomically.
func (s *State) do(f func(*stateFile)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	unlock, err := config.LockFile(s.Path) // another process (TUI/web) may have written since we loaded
	if err != nil {
		return err
	}
	defer unlock()
	var raw map[string]json.RawMessage // keys this build does not know (written by another version) are kept
	if b, err := os.ReadFile(s.Path); err == nil {
		var cur stateFile
		if json.Unmarshal(b, &cur) == nil {
			s.d = cur
			_ = json.Unmarshal(b, &raw)
		}
	}
	f(&s.d)
	b, err := json.Marshal(s.d)
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		var mine map[string]json.RawMessage
		if json.Unmarshal(b, &mine) != nil {
			return errors.New("state: cannot merge")
		}
		t := reflect.TypeOf(stateFile{})
		for i := 0; i < t.NumField(); i++ { // drop the keys we own, then lay ours over what is left
			delete(raw, strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
		}
		for k, v := range mine {
			raw[k] = v
		}
		if b, err = json.Marshal(raw); err != nil {
			return err
		}
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", " "); err != nil {
		return err
	}
	return config.WriteFileAtomic(s.Path, out.Bytes())
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

// Folds returns the folded statement indexes remembered for an editor key.
func (s *State) Folds(key string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.d.Folds[key]...)
}

// SetFolds remembers which statements are folded; an empty list forgets the key.
func (s *State) SetFolds(key string, folded []int) error {
	return s.do(func(d *stateFile) {
		if len(folded) == 0 {
			delete(d.Folds, key)
			return
		}
		if d.Folds == nil {
			d.Folds = map[string][]int{}
		}
		d.Folds[key] = append([]int(nil), folded...)
	})
}

// OpenTabs returns the tabs saved for a connection.
func (s *State) OpenTabs(conn string) []OpenTab {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]OpenTab(nil), s.d.Tabs[conn]...)
}

// SetOpenTabs saves the tabs to restore for a connection; an empty list forgets them.
func (s *State) SetOpenTabs(conn string, tabs []OpenTab) error {
	return s.do(func(d *stateFile) {
		if len(tabs) == 0 {
			delete(d.Tabs, conn)
			return
		}
		if d.Tabs == nil {
			d.Tabs = map[string][]OpenTab{}
		}
		d.Tabs[conn] = append([]OpenTab(nil), tabs...)
	})
}

// ItemGroup is a user-made sidebar group; Items are "schema.name".
type ItemGroup struct {
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

// BackupOptions returns the backup/restore dialog options last used on a connection.
func (s *State) BackupOptions(conn string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]string{}
	for k, v := range s.d.Backup[conn] {
		m[k] = v
	}
	return m
}

// SetBackupOptions merges opts into the remembered ones (both UIs share the map); an empty opts forgets the connection.
func (s *State) SetBackupOptions(conn string, opts map[string]string) error {
	return s.do(func(d *stateFile) {
		if len(opts) == 0 {
			delete(d.Backup, conn)
			return
		}
		if d.Backup == nil {
			d.Backup = map[string]map[string]string{}
		}
		m := map[string]string{}
		for k, v := range d.Backup[conn] {
			m[k] = v
		}
		for k, v := range opts {
			m[k] = v
		}
		d.Backup[conn] = m
	})
}

// RecentFiles lists files opened with open_file, newest first.
func (s *State) RecentFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.d.Files...)
}

// AddRecentFile moves path to the front and keeps at most max entries (max<=0 forgets them all).
func (s *State) AddRecentFile(path string, max int) error {
	return s.do(func(d *stateFile) {
		out := []string{path}
		for _, f := range d.Files {
			if f != path {
				out = append(out, f)
			}
		}
		if max < 0 {
			max = 0
		}
		if len(out) > max {
			out = out[:max]
		}
		d.Files = out
	})
}

// ConnectionSort is the connection list order ("name", "tag" or "driver"); "" means the config default.
func (s *State) ConnectionSort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Sort
}

func (s *State) SetConnectionSort(by string) error { return s.do(func(d *stateFile) { d.Sort = by }) }

// Groups returns the custom item groups of a connection.
func (s *State) Groups(conn string) []ItemGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ItemGroup, 0, len(s.d.Groups[conn]))
	for _, g := range s.d.Groups[conn] {
		out = append(out, ItemGroup{g.Name, append([]string(nil), g.Items...)})
	}
	return out
}

// SetGroups replaces a connection's groups; an empty list forgets them.
func (s *State) SetGroups(conn string, groups []ItemGroup) error {
	return s.do(func(d *stateFile) {
		if len(groups) == 0 {
			delete(d.Groups, conn)
			return
		}
		if d.Groups == nil {
			d.Groups = map[string][]ItemGroup{}
		}
		d.Groups[conn] = groups
	})
}
