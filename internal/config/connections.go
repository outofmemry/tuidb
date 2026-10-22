package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Driver names. Aliases accepted in URLs are mapped in ParseURL.
const (
	Postgres   = "postgres"
	MySQL      = "mysql"
	ClickHouse = "clickhouse"
	Redis      = "redis"
	Mongo      = "mongodb"
)

var Drivers = []string{Postgres, MySQL, ClickHouse, Redis, Mongo}

var defaultPorts = map[string]int{Postgres: 5432, MySQL: 3306, ClickHouse: 9000, Redis: 6379, Mongo: 27017}

func DefaultPort(driver string) int { return defaultPorts[driver] }

type SSH struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port,omitempty"`
	User     string `toml:"user"`
	Key      string `toml:"key,omitempty"`
	Password string `toml:"password,omitempty"`
}

type Connection struct {
	Name        string   `toml:"name"`
	Driver      string   `toml:"driver"`
	Host        string   `toml:"host"`
	Port        int      `toml:"port,omitempty"`
	User        string   `toml:"user,omitempty"`
	Password    string   `toml:"password,omitempty"`
	PasswordCmd string   `toml:"password_cmd,omitempty"` // stdout becomes the password
	Database    string   `toml:"database,omitempty"`
	Tag         string   `toml:"tag,omitempty"`
	Color       string   `toml:"color,omitempty"`
	SafeMode    string   `toml:"safe_mode,omitempty"`
	Group       string   `toml:"group,omitempty"` // "A/B" nests
	SSLMode     string   `toml:"ssl_mode,omitempty"`
	SSLCert     string   `toml:"ssl_cert,omitempty"`
	SSLKey      string   `toml:"ssl_key,omitempty"`
	SSLCA       string   `toml:"ssl_ca,omitempty"`
	Bootstrap   []string `toml:"bootstrap,omitempty"`
	// Options carries driver-specific extras from URL query params / config (e.g. authSource for mongodb).
	Options map[string]string `toml:"options,omitempty"`
	SSH     *SSH              `toml:"ssh,omitempty"`
}

type connectionsFile struct {
	Connection []Connection `toml:"connection"`
}

// Validate checks the fields every driver needs.
func (c Connection) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("connection needs a name")
	}
	for _, d := range Drivers {
		if c.Driver == d {
			return nil
		}
	}
	return fmt.Errorf("connection %q: unknown driver %q (want %s)", c.Name, c.Driver, strings.Join(Drivers, "|"))
}

func (c Connection) EffectivePort() int {
	if c.Port != 0 {
		return c.Port
	}
	return DefaultPort(c.Driver)
}

// ResolvePassword returns the literal password, or the stdout of password_cmd.
func (c Connection) ResolvePassword(ctx context.Context) (string, error) {
	if c.PasswordCmd == "" {
		return c.Password, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", c.PasswordCmd)
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("password_cmd for %q failed: %w: %s", c.Name, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

func ConnectionsPath() string { return filepath.Join(ConfigDir(), "connections.toml") }

func LoadConnections(path string) ([]Connection, error) {
	var f connectionsFile
	if _, err := toml.DecodeFile(path, &f); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, c := range f.Connection {
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("%s: duplicate connection name %q", path, c.Name)
		}
		seen[c.Name] = true
	}
	return f.Connection, nil
}

// SaveConnections writes atomically with 0600 perms (passwords are stored in plain text).
func SaveConnections(path string, conns []Connection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(connectionsFile{Connection: conns}); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".connections-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var urlSchemes = map[string]string{
	"postgres": Postgres, "postgresql": Postgres, "mysql": MySQL, "mariadb": MySQL,
	"clickhouse": ClickHouse, "redis": Redis, "rediss": Redis, "mongodb": Mongo,
}

// SSLModes are the values of Connection.SSLMode.
var SSLModes = []string{"disable", "prefer", "require", "verify-ca", "verify-full", "verify-identity"}

// sslFromQuery reads TLS settings from a URL query in tuidb's own spelling (sslmode=) and the
// engines' native ones, so a URL pasted from another tool does not silently connect in plaintext.
// It returns the mode and the query keys it consumed.
func sslFromQuery(driver string, q url.Values) (string, []string) {
	truthy := func(v string) bool { return v == "true" || v == "1" || v == "yes" }
	if v := q.Get("sslmode"); v != "" {
		return strings.ToLower(v), []string{"sslmode"}
	}
	switch driver {
	case MySQL:
		if v := q.Get("ssl-mode"); v != "" {
			return map[string]string{"DISABLED": "disable", "PREFERRED": "prefer", "REQUIRED": "require", "VERIFY_CA": "verify-ca", "VERIFY_IDENTITY": "verify-identity"}[strings.ToUpper(v)], []string{"ssl-mode"}
		}
		if v := q.Get("tls"); v != "" {
			if truthy(v) || v == "skip-verify" || v == "preferred" {
				return map[bool]string{true: "prefer", false: "require"}[v == "preferred"], []string{"tls"}
			}
			return "disable", []string{"tls"}
		}
	case ClickHouse:
		if v := q.Get("secure"); v != "" {
			if truthy(v) {
				return "require", []string{"secure"}
			}
			return "disable", []string{"secure"}
		}
	case Mongo:
		for _, k := range []string{"tls", "ssl"} {
			if v := q.Get(k); v != "" {
				if truthy(v) {
					return "require", []string{k}
				}
				return "disable", []string{k}
			}
		}
	}
	return "", nil
}

// ParseURL imports driver://user:pass@host:port/db?name=..&env=..&safeModeLevel=..; the ssh and
// ssl/tag options use TablePlus's query names where they map (name, env, statusColor).
func ParseURL(raw string) (Connection, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Connection{}, fmt.Errorf("bad URL: %w", err)
	}
	if strings.EqualFold(u.Scheme, "mongodb+srv") {
		return Connection{}, fmt.Errorf("mongodb+srv:// is not supported (DNS seed lists); use mongodb://host:port")
	}
	driver, ok := urlSchemes[strings.ToLower(u.Scheme)]
	if !ok {
		return Connection{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	c := Connection{Driver: driver, Host: u.Hostname(), Database: strings.TrimPrefix(u.Path, "/")}
	if p := u.Port(); p != "" {
		if c.Port, err = strconv.Atoi(p); err != nil {
			return c, fmt.Errorf("bad port %q", p)
		}
	}
	if u.User != nil {
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
	}
	q := u.Query()
	if driver == Redis && c.Database != "" && c.User == "" && c.Password == "" {
		// keep as-is: redis://host/3 selects db 3
	}
	if strings.ToLower(u.Scheme) == "rediss" && c.SSLMode == "" {
		c.SSLMode = "require"
	}
	c.Name = q.Get("name")
	c.Tag = q.Get("env")
	mode, consumed := sslFromQuery(driver, q)
	if mode != "" {
		ok := false
		for _, m := range SSLModes {
			ok = ok || m == mode
		}
		if !ok {
			return c, fmt.Errorf("unknown ssl mode %q (want %s)", mode, strings.Join(SSLModes, "|"))
		}
		c.SSLMode = mode
	}
	for k, v := range q {
		switch k {
		case "name", "env", "statusColor":
		default:
			if contains(consumed, k) {
				continue
			}
			if c.Options == nil {
				c.Options = map[string]string{}
			}
			c.Options[k] = v[0]
		}
	}
	if col := q.Get("statusColor"); col != "" {
		c.Color = "#" + strings.TrimPrefix(col, "#")
	}
	if c.Name == "" {
		c.Name = c.Host
		if c.Database != "" {
			c.Name += "/" + c.Database
		}
		if c.Name == "" {
			c.Name = driver
		}
	}
	return c, c.Validate()
}

// FormatURL is the inverse of ParseURL ("Copy as URL"); it includes the password.
func (c Connection) FormatURL() string {
	scheme := c.Driver
	if scheme == Postgres {
		scheme = "postgresql"
	}
	u := url.URL{Scheme: scheme, Host: c.Host, Path: "/" + c.Database}
	if c.Port != 0 {
		u.Host = fmt.Sprintf("%s:%d", c.Host, c.Port)
	}
	if c.User != "" {
		u.User = url.UserPassword(c.User, c.Password)
		if c.Password == "" {
			u.User = url.User(c.User)
		}
	}
	q := url.Values{}
	for k, v := range c.Options {
		q.Set(k, v)
	}
	if c.SSLMode != "" {
		q.Set("sslmode", c.SSLMode) // tuidb's own spelling; ParseURL also reads the native ones
	}
	q.Set("name", c.Name)
	if c.Tag != "" {
		q.Set("env", c.Tag)
	}
	if c.Color != "" {
		q.Set("statusColor", strings.TrimPrefix(c.Color, "#"))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Tags: TablePlus defaults, recolourable by Connection.Color only at the connection level.
var DefaultTags = []string{"local", "testing", "development", "staging", "production"}

// Groups returns the sorted distinct group paths ("A", "A/B").
func Groups(conns []Connection) []string {
	set := map[string]bool{}
	for _, c := range conns {
		for g := c.Group; g != ""; {
			set[g] = true
			i := strings.LastIndex(g, "/")
			if i < 0 {
				break
			}
			g = g[:i]
		}
	}
	out := make([]string, 0, len(set))
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}
