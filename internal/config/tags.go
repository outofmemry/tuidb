package config

import "fmt"

// Tags maps every tag name (the defaults plus [[tag]]) to its colour; "" keeps the UI palette's own
// colour for that name. A later [[tag]] with a default's name recolours it.
func Tags(cfg []Tag) map[string]string {
	m := map[string]string{}
	for _, n := range DefaultTags {
		m[n] = ""
	}
	for _, t := range cfg {
		m[t.Name] = t.Color
	}
	return m
}

// ValidateTags rejects empty and repeated [[tag]] names.
func ValidateTags(cfg []Tag) error {
	seen := map[string]bool{}
	for _, t := range cfg {
		if t.Name == "" {
			return fmt.Errorf("[[tag]] needs a name")
		}
		if seen[t.Name] {
			return fmt.Errorf("duplicate [[tag]] name %q", t.Name)
		}
		seen[t.Name] = true
	}
	return nil
}
