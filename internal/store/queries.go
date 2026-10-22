package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tuidb/internal/config"
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
}

type Queries struct{ Root string }

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
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid query path %q", rel)
	}
	return filepath.Join(s.Root, clean), nil
}

func fileName(name string) string {
	return unsafeName.ReplaceAllString(strings.TrimSpace(name), "_") + ".sql"
}

// Save writes (or overwrites) a query and returns it. folder may be "" or "a/b".
func (s *Queries) Save(folder, name string, keywords []string, sql string) (Query, error) {
	if strings.TrimSpace(name) == "" {
		return Query{}, errors.New("a saved query needs a name")
	}
	rel := filepath.Join(filepath.FromSlash(folder), fileName(name))
	p, err := s.abs(rel)
	if err != nil {
		return Query{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return Query{}, err
	}
	if err := config.WriteFileAtomic(p, []byte(header(name, keywords)+sql)); err != nil {
		return Query{}, err
	}
	return parse(rel, header(name, keywords)+sql), nil
}

func (s *Queries) Read(path string) (Query, error) {
	p, err := s.abs(path)
	if err != nil {
		return Query{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Query{}, err
	}
	return parse(path, string(b)), nil
}

// Update rewrites the SQL of an existing query in place, keeping name and keywords (editor auto-save).
func (s *Queries) Update(path, sql string) error {
	q, err := s.Read(path)
	if err != nil {
		return err
	}
	p, _ := s.abs(path)
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
	err := filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
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
		rel, _ := filepath.Rel(s.Root, p)
		out = append(out, parse(rel, string(b)))
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
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
