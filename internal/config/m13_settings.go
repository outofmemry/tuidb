package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/crypto/bcrypt"

	"webtuidb/internal/keymap"
)

// Field is one editable setting for the settings editor (terminal and web). Value is the current value as text;
// lists are comma-separated.
type Field struct {
	Key     string   `json:"key"`  // "table.filter_state"
	Type    string   `json:"type"` // string | bool | int | list | enum
	Enum    []string `json:"enum,omitempty"`
	Doc     string   `json:"doc"`
	Value   string   `json:"value"`
	Section string   `json:"section"`
}

// secretKeys are never offered by Schema (set a passcode with -set-passcode, the web password in the file).
var secretKeys = map[string]bool{"web.password": true, "security.passcode": true}

// sections lists the Config tables the editor covers, in display order. ai, mcp, [[tag]], [[source]] and [keys] are edited elsewhere.
var sections = []string{"general", "editor", "table", "csv", "sidebar", "theme", "export", "web", "queries", "security"}

// Schema describes every setting of the editable sections, from the struct tags.
func Schema(c Config) []Field {
	var out []Field
	rv := reflect.ValueOf(c)
	rt := rv.Type()
	for _, sec := range sections {
		for i := 0; i < rt.NumField(); i++ {
			if rt.Field(i).Tag.Get("toml") != sec {
				continue
			}
			st, sv := rt.Field(i).Type, rv.Field(i)
			for j := 0; j < st.NumField(); j++ {
				name := strings.Split(st.Field(j).Tag.Get("toml"), ",")[0]
				key := sec + "." + name
				if name == "" || name == "-" || secretKeys[key] || st.Field(j).Type.Kind() == reflect.Map {
					continue
				}
				f := Field{Key: key, Section: sec, Doc: settingDocs[key]}
				fv := sv.Field(j)
				switch st.Field(j).Type.Kind() {
				case reflect.Bool:
					f.Type, f.Value = "bool", strconv.FormatBool(fv.Bool())
				case reflect.Int, reflect.Int64:
					f.Type, f.Value = "int", strconv.FormatInt(fv.Int(), 10)
				case reflect.Slice:
					f.Type, f.Value = "list", strings.Join(fv.Interface().([]string), ",")
				default:
					f.Type, f.Value = "string", fv.String()
				}
				if e, ok := enums[key]; ok {
					f.Type, f.Enum = "enum", e
				}
				if key == "theme.name" {
					f.Type, f.Enum = "enum", append(append([]string{}, ThemeNames...), InstalledThemes()...)
				}
				out = append(out, f)
			}
		}
	}
	return out
}

// rewrite reads the TOML at path into a generic map (a missing file is empty), lets edit change it, checks the
// result by loading it as a Config, and replaces the file atomically (0600). Unknown-but-valid tables such as
// [ai.providers.x] and [[tag]] survive because the map keeps everything it read.
// shortcut: comments in the file are not kept (the file gets a one-line note), keep them with a TOML AST editor.
func rewrite(path string, edit func(m map[string]any) error) error {
	m := map[string]any{}
	if _, err := toml.DecodeFile(path, &m); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := edit(m); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# Rewritten by webtuidb settings; comments from earlier versions of this file are not kept.\n")
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if _, err := Load(tmp.Name()); err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), tmp.Name(), path))
	}
	return os.Rename(tmp.Name(), path)
}

func table(m map[string]any, name string) map[string]any {
	t, ok := m[name].(map[string]any)
	if !ok {
		t = map[string]any{}
		m[name] = t
	}
	return t
}

// ApplyValues sets "section.key" -> text values (as Schema renders them) in the file at path. Nothing is written
// when a value does not fit its type or the resulting config does not load.
func ApplyValues(path string, values map[string]string) error {
	types := map[string]Field{}
	for _, f := range Schema(Defaults()) {
		types[f.Key] = f
	}
	return rewrite(path, func(m map[string]any) error {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f, ok := types[k]
			if !ok {
				return fmt.Errorf("unknown setting %q", k)
			}
			raw := values[k]
			var v any = raw
			switch f.Type {
			case "bool":
				b, err := strconv.ParseBool(strings.TrimSpace(raw))
				if err != nil {
					return fmt.Errorf("%s: %q is not true or false", k, raw)
				}
				v = b
			case "int":
				n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				if err != nil {
					return fmt.Errorf("%s: %q is not a whole number", k, raw)
				}
				v = n
			case "list":
				list := []string{}
				for _, p := range strings.Split(raw, ",") {
					if p = strings.TrimSpace(p); p != "" {
						list = append(list, p)
					}
				}
				v = list
			}
			sec, name, _ := strings.Cut(k, ".")
			table(m, sec)[name] = v
		}
		return nil
	})
}

// WriteKeys replaces the [keys] table with keys (action -> key specs). The keymap is built first, so a conflict or an
// unknown action is returned and nothing is written. An empty list resets that action to its default.
// plugins are the installed plugins' bindings: a new key that takes a plugin's shortcut is refused too.
func WriteKeys(path string, keys map[string][]string, plugins ...keymap.Binding) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	_, before, _ := cfg.KeymapChecked(plugins)
	cfg.KeyOverrides = map[string][]string{}
	for a, specs := range keys {
		if len(specs) > 0 {
			cfg.KeyOverrides[a] = specs
		}
	}
	if _, err := cfg.keymapWith(nil, true); err != nil {
		return err
	}
	if len(plugins) > 0 {
		_, after, err := cfg.KeymapChecked(plugins)
		if err != nil {
			return err
		}
		for _, c := range after {
			if !slices.Contains(before, c) {
				return errors.New(c)
			}
		}
	}
	return rewrite(path, func(m map[string]any) error {
		t := map[string]any{}
		for a, specs := range cfg.KeyOverrides {
			if len(specs) == 1 {
				t[a] = specs[0]
			} else {
				t[a] = specs
			}
		}
		if len(t) == 0 {
			delete(m, "keys")
		} else {
			m["keys"] = t
		}
		return nil
	})
}

// HashPasscode returns the bcrypt hash to store in [security] passcode.
func HashPasscode(p string) (string, error) {
	if p == "" {
		return "", errors.New("the passcode must not be empty")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPasscode reports whether passcode matches hash. An empty hash means no passcode is set, so it never matches.
func CheckPasscode(hash, passcode string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(passcode)) == nil
}

// Preset is a ready-made password_cmd for a secret store; <name> in Command is the entry name to replace.
type Preset struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Note    string `json:"note"`
}

// PasswordCmdPresets are the secret-store commands offered in the connection form.
func PasswordCmdPresets() []Preset {
	return []Preset{
		{"macOS Keychain", "security find-generic-password -s webtuidb -a <name> -w", "add it with: security add-generic-password -s webtuidb -a <name> -w"},
		{"Linux secret-tool", "secret-tool lookup service webtuidb account <name>", "add it with: secret-tool store --label=webtuidb service webtuidb account <name>"},
		{"pass", "pass show webtuidb/<name>", "add it with: pass insert webtuidb/<name>"},
		{"1Password", `op read "op://Private/<name>/password"`, "needs the op CLI signed in"},
	}
}

// HasComments reports whether the config file holds comments of its own (not the note rewrite adds), whole-line or
// after a value (`key = 1 # note`). Saving through ApplyValues or WriteKeys re-encodes the TOML and drops them.
func HasComments(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(b)
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '#':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				j = len(s) - i
			}
			if !strings.HasPrefix(s[i:i+j], "# Rewritten by webtuidb") {
				return true
			}
			i += j
		case c == '"' || c == '\'':
			q := string(c)
			multi := strings.HasPrefix(s[i:], strings.Repeat(q, 3))
			if multi {
				q = strings.Repeat(q, 3)
			}
			i += len(q)
			for i < len(s) {
				if c == '"' && s[i] == '\\' {
					i += 2
					continue
				}
				if strings.HasPrefix(s[i:], q) {
					i += len(q)
					break
				}
				if !multi && s[i] == '\n' {
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return false
}
