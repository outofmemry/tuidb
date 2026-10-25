package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"webtuidb/internal/config"
)

// Query is a saved query ("favorite"): a plain .sql file whose first lines may be
//
//	-- name: Recent orders
//	-- keywords: ro, recent
type Query struct {
	Path     string // relative to the store root, slash separated
	Folder   string
	Name     string
	Keywords []string
	SQL      string
	Shared   bool      // from a [queries] shared_dirs folder: read-only, Path is absolute
	Modified time.Time // shortcut: Created is not tracked (no portable birth time), SortQueries "created" uses Modified too
}

type Queries struct {
	Root   string
	shared []string
}

// AddDir lists the .sql files under path beside the main folder, read-only ([queries] shared_dirs).
// A relative path or "~" is made absolute, since shared queries are addressed by absolute path.
func (s *Queries) AddDir(path string) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	s.shared = append(s.shared, path)
}

// sharedAbs reports whether p is an absolute path inside one of the shared folders.
func (s *Queries) sharedAbs(p string) bool {
	for _, d := range s.shared {
		if r, err := filepath.Rel(d, p); err == nil && filepath.IsAbs(p) && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// SortQueries orders qs by "name", "created" or "modified" (newest first); folders stay grouped.
func SortQueries(qs []Query, by string) {
	sort.SliceStable(qs, func(i, j int) bool {
		a, b := qs[i], qs[j]
		if a.Folder != b.Folder {
			return a.Folder < b.Folder
		}
		if by == "created" || by == "modified" {
			if !a.Modified.Equal(b.Modified) {
				return a.Modified.After(b.Modified)
			}
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// ImportSQLFile reads a .sql file as a query: its `-- name:` header, else the file name. Save it to keep it.
func ImportSQLFile(path string) (Query, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Query{}, err
	}
	q := parse(filepath.Base(path), string(b))
	q.Path = "" // not in a store yet
	return q, nil
}

func header(name string, keywords []string) string {
	s := "-- name: " + name + "\n"
	if len(keywords) > 0 {
		s += "-- keywords: " + strings.Join(keywords, ", ") + "\n"
	}
	return s
}

func parse(path, raw string) Query {
	q := Query{Path: filepath.ToSlash(path), Folder: filepath.ToSlash(filepath.Dir(path))}
	if q.Folder == "." {
		q.Folder = ""
	}
	q.Name = strings.TrimSuffix(filepath.Base(path), ".sql")
	rest := raw
	for {
		line, after, found := strings.Cut(rest, "\n")
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(strings.ToLower(l), "-- name:"):
			q.Name = strings.TrimSpace(l[len("-- name:"):])
		case strings.HasPrefix(strings.ToLower(l), "-- keywords:"):
			for _, k := range strings.Split(l[len("-- keywords:"):], ",") {
				if k = strings.TrimSpace(k); k != "" {
					q.Keywords = append(q.Keywords, k)
				}
			}
		default:
			q.SQL = rest
			return q
		}
		if !found {
			return q
		}
		rest = after
	}
}

// safe keeps a user-typed name from escaping Root.
func (s *Queries) abs(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid query path %q", rel)
	}
	return filepath.Join(s.Root, clean), nil
}

func fileName(name string) string {
	return unsafeName.ReplaceAllString(strings.TrimSpace(name), "_") + ".sql"
}

// ErrExists is what SaveIfAbsent returns when the query's file is already there.
var ErrExists = errors.New("exists")

// Save writes (or overwrites) a query and returns it. folder may be "" or "a/b".
func (s *Queries) Save(folder, name string, keywords []string, sql string) (Query, error) {
	return s.save(folder, name, keywords, sql, true)
}

// SaveIfAbsent is Save that returns ErrExists instead of replacing a file (imports, "save as").
func (s *Queries) SaveIfAbsent(folder, name string, keywords []string, sql string) (Query, error) {
	return s.save(folder, name, keywords, sql, false)
}

func (s *Queries) save(folder, name string, keywords []string, sql string, overwrite bool) (Query, error) {
	if strings.TrimSpace(name) == "" {
		return Query{}, errors.New("a saved query needs a name")
	}
	if strings.ContainsAny(name+strings.Join(keywords, ","), "\r\n") { // a newline would end the "-- name:" comment and leave live SQL
		return Query{}, errors.New("a query name and its keywords cannot contain a line break")
	}
	rel := filepath.Join(filepath.FromSlash(folder), fileName(name))
	p, err := s.abs(rel)
	if err != nil {
		return Query{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return Query{}, err
	}
	if !overwrite { // create-exclusive, so two tabs saving the same new name cannot both succeed
		unlock, err := config.LockFile(p)
		if err != nil {
			return Query{}, err
		}
		defer unlock()
		if _, err := os.Stat(p); err == nil {
			return Query{}, ErrExists
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return Query{}, ErrExists
		}
		if err != nil {
			return Query{}, err
		}
		f.Close()
	} else if b, err := os.ReadFile(p); err == nil { // same file name, different query: refuse instead of clobbering
		if old := parse(rel, string(b)); old.Name != strings.TrimSpace(name) && old.Name != name {
			return Query{}, fmt.Errorf("a different saved query %q already uses the file name %s", old.Name, filepath.Base(p))
		}
	}
	if err := config.WriteFileAtomic(p, []byte(header(name, keywords)+sql)); err != nil {
		if !overwrite {
			os.Remove(p) // the empty placeholder
		}
		return Query{}, err
	}
	return parse(rel, header(name, keywords)+sql), nil
}

func (s *Queries) Read(path string) (Query, error) {
	shared := s.sharedAbs(filepath.FromSlash(path))
	p := filepath.FromSlash(path)
	if !shared {
		var err error
		if p, err = s.abs(path); err != nil {
			return Query{}, err
		}
	}
	if shared && !strings.HasSuffix(p, ".sql") { // a shared directory is the user's own tree: only .sql files leave it
		return Query{}, fmt.Errorf("%s is not a .sql file", path)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Query{}, err
	}
	q := parse(path, string(b))
	q.Shared = shared
	return q, nil
}

// Update rewrites the SQL of an existing query in place, keeping name and keywords (editor auto-save).
func (s *Queries) Update(path, sql string) error {
	p, err := s.abs(path)
	if err != nil {
		if s.sharedAbs(filepath.FromSlash(path)) {
			return errors.New("shared queries are read-only")
		}
		return err
	}
	unlock, err := config.LockFile(p) // read the header and rewrite as one step: the other UI may be saving too
	if err != nil {
		return err
	}
	defer unlock()
	q, err := s.Read(path)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(p, []byte(header(q.Name, q.Keywords)+sql))
}

func (s *Queries) Delete(path string) error {
	p, err := s.abs(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(p) // also removes a folder
}

func (s *Queries) Move(path, folder string) error {
	from, err := s.abs(path)
	if err != nil {
		return err
	}
	to, err := s.abs(filepath.Join(filepath.FromSlash(folder), filepath.Base(path)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func (s *Queries) MkFolder(folder string) error {
	p, err := s.abs(folder)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o700)
}

// List returns every saved query, folders first-class via Folder, sorted by folder then name.
func (s *Queries) List() ([]Query, error) {
	var out []Query
	walk := func(root string, shared bool) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".sql") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			q := parse(rel, string(b))
			if shared { // read-only: addressed by absolute path, grouped under the folder's own name
				q.Path, q.Shared = filepath.ToSlash(p), true
				q.Folder = strings.Trim(filepath.ToSlash(filepath.Join(filepath.Base(root), filepath.Dir(rel))), "./")
			}
			if info, err := d.Info(); err == nil {
				q.Modified = info.ModTime()
			}
			out = append(out, q)
			return nil
		})
	}
	err := walk(s.Root, false)
	for _, d := range s.shared {
		if err == nil {
			err = walk(d, true)
		}
	}
	SortQueries(out, "name")
	return out, err
}

// Keywords maps each bound keyword to its query, for autocomplete expansion.
func Keywords(qs []Query) map[string]Query {
	m := map[string]Query{}
	for _, q := range qs {
		for _, k := range q.Keywords {
			m[k] = q
		}
	}
	return m
}
