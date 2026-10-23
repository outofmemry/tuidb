package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PasswordAsk as a connection password means "prompt on every connect" (TablePlus `password = "ask"`).
const PasswordAsk = "ask"

// LoadAllConnections reads connections.toml and every *.toml inside each [[source]] folder (Connection Sources).
// Source files use the connections.toml format and are read-only: SaveConnections only writes the main file.
// A connection from a source carries Source = the file it came from.
func LoadAllConnections(path string, sources []Source) ([]Connection, error) {
	all, err := LoadConnections(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range all {
		seen[c.Name] = true
	}
	for _, src := range sources {
		dir := os.ExpandEnv(src.Path)
		if strings.HasPrefix(dir, "~/") {
			if h, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(h, dir[2:])
			}
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.toml"))
		sort.Strings(files)
		for _, f := range files {
			cs, err := loadConnections(f, false)
			if err != nil {
				return nil, fmt.Errorf("source %s: %w", f, err)
			}
			for _, c := range cs {
				if seen[c.Name] {
					continue // the main file wins
				}
				c.Source = f
				seen[c.Name] = true
				all = append(all, c)
			}
		}
	}
	return all, nil
}
