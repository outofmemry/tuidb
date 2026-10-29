package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"webtuidb/internal/safemode"
)

// Driver names. Aliases accepted in URLs are mapped in ParseURL.
const (
	Postgres   = "postgres"
	MySQL      = "mysql"
	ClickHouse = "clickhouse"
	Redis      = "redis"
	Mongo      = "mongodb"
	MariaDB    = "mariadb"
	Cockroach  = "cockroachdb"
	SQLite     = "sqlite"
	DuckDB     = "duckdb"
	Cassandra  = "cassandra"
	DynamoDB   = "dynamodb"
	D1         = "d1" // Cloudflare D1 over the REST API: Host = account id, Password = API token, Database = database name or id

	// Capability profiles of the postgres / mysql drivers.
	Redshift    = "redshift"
	Greenplum   = "greenplum"
	Vertica     = "vertica"
	SingleStore = "singlestore"
)

// Cockroach, SQLite, DuckDB and D1 have no driver (their stubs are gone), so they are not in Drivers;
// the constants stay because the UIs still name them.
var Drivers = []string{Postgres, MySQL, MariaDB, Redshift, Greenplum, Vertica, SingleStore, ClickHouse, Cassandra, DynamoDB, Redis, Mongo}

// 0 means "no port": DynamoDB (AWS endpoint unless host is set).
var defaultPorts = map[string]int{Postgres: 5432, MySQL: 3306, MariaDB: 3306, Redshift: 5439, Greenplum: 5432, Vertica: 5433, SingleStore: 3306, ClickHouse: 9000, Cassandra: 9042, Redis: 6379, Mongo: 27017}

// FileBased engines open a local file named by Connection.Database; they have no host, login or SSH tunnel.
func FileBased(driver string) bool { return driver == SQLite || driver == DuckDB }

func DefaultPort(driver string) int { return defaultPorts[driver] }

type SSH struct {
	Host      string `toml:"host"`
	Port      int    `toml:"port,omitempty"`
	User      string `toml:"user"`
	Key       string `toml:"key,omitempty"`
	Password  string `toml:"password,omitempty"`
	ProxyJump string `toml:"proxy_jump,omitempty"` // "user@bastion:22" hop, or taken from ~/.ssh/config when empty (M5)
}

type Connection struct {
	Name        string            `toml:"name"`
	Driver      string            `toml:"driver"`
	Host        string            `toml:"host"`
	Port        int               `toml:"port,omitempty"`
	User        string            `toml:"user,omitempty"`
	Password    string            `toml:"password,omitempty"`
	PasswordCmd string            `toml:"password_cmd,omitempty"` // stdout becomes the password
	Database    string            `toml:"database,omitempty"`
	Tag         string            `toml:"tag,omitempty"`
	Color       string            `toml:"color,omitempty"`
	SafeMode    string            `toml:"safe_mode,omitempty"`
	Group       string            `toml:"group,omitempty"` // "A/B" nests
	SSLMode     string            `toml:"ssl_mode,omitempty"`
	SSLCert     string            `toml:"ssl_cert,omitempty"`
	SSLKey      string            `toml:"ssl_key,omitempty"`
	SSLCA       string            `toml:"ssl_ca,omitempty"`
	Bootstrap   []string          `toml:"bootstrap,omitempty"`
	Encoding    string            `toml:"encoding,omitempty"`    // client encoding override: PostgreSQL client_encoding, MySQL charset
	ReadOnly    bool              `toml:"read_only,omitempty"`   // refuse every non-read-only statement on this connection (M5)
	SocksProxy  string            `toml:"socks_proxy,omitempty"` // "host:port" SOCKS5 proxy for the database dial (M5)
	IAMAuth     bool              `toml:"iam_auth,omitempty"`    // AWS RDS IAM token instead of the password (postgres, mysql) (M5)
	Unusable    string            `toml:"-"`                     // why the connection cannot be opened (e.g. its driver was removed); it is kept and saved as is
	Source      string            `toml:"-"`                     // the [[source]] file this connection came from; "" = connections.toml
	Tools       map[string]string `toml:"tools,omitempty"`       // dump tool paths: pg_dump, pg_restore, psql, mysqldump, mysql, mongodump... (M7)
	// DriverVersion and LazyLoad come from the URL options driverVersion / lazyload (TablePlus spelling).
	DriverVersion string `toml:"driver_version,omitempty"`
	LazyLoad      bool   `toml:"lazy_load,omitempty"`
	// Open is the table a URL asked to open right after connecting; never saved.
	Open *OpenTarget `toml:"-"`
	// Options carries driver-specific extras from URL query params / config (e.g. authSource for mongodb).
	Options map[string]string `toml:"options,omitempty"`
	SSH     *SSH              `toml:"ssh,omitempty"`
}

// OpenTarget is the table (and optional filter) named by the URL options schema, table, column,
// operation, value and condition.
type OpenTarget struct {
	Schema, Table, Column, Operation, Value, Condition string
}

type connectionsFile struct {
	Connection []Connection `toml:"connection"`
}

// Validate checks the fields every driver needs.
func (c Connection) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("connection needs a name")
	}
	ok := false
	for _, d := range Drivers {
		ok = ok || c.Driver == d
	}
	if !ok {
		return fmt.Errorf("connection %q: unknown driver %q (want %s)", c.Name, c.Driver, strings.Join(Drivers, "|"))
	}
	// A typo here must not fail open: both settings decide whether a write is confirmed / encrypted.
	if _, err := safemode.Parse(c.SafeMode); err != nil {
		return fmt.Errorf("connection %q: safe_mode: %w", c.Name, err)
	}
	if c.Encoding != "" && !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(c.Encoding) {
		return fmt.Errorf("connection %q: encoding %q must be a plain charset name", c.Name, c.Encoding)
	}
	if c.SSLMode != "" && !contains(SSLModes, c.SSLMode) {
		return fmt.Errorf("connection %q: unknown ssl_mode %q (want %s)", c.Name, c.SSLMode, strings.Join(SSLModes, "|"))
	}
	if c.SSH != nil && c.SSH.Host != "" && (c.SSH.Port < 0 || c.SSH.Port > 65535) {
		return fmt.Errorf("connection %q: ssh port must be 1-65535", c.Name)
	}
	if c.Driver == DynamoDB {
		if _, _, _, err := SplitEndpointHost(c.Host); err != nil {
			return fmt.Errorf("connection %q: host: %w", c.Name, err)
		}
	}
	return nil
}

// SplitEndpointHost reads the host field of an engine that talks HTTP (DynamoDB): a bare host
// ("localhost", "localhost:8000", "[::1]:8000") or one with a scheme ("http://localhost:8000",
// optionally ending in "/"). The scheme is returned when present ("" otherwise) and then wins over the
// scheme options; the port is "" unless the host carried one. Anything else (another scheme, a path,
// credentials) is an error, so "http://http://..." never reaches the network as a DNS failure.
func SplitEndpointHost(host string) (scheme, hostname, port string, err error) {
	h := strings.TrimSpace(host)
	if h == "" {
		return "", "", "", nil
	}
	if i := strings.Index(h, "://"); i >= 0 {
		scheme = strings.ToLower(h[:i])
		if scheme != "http" && scheme != "https" {
			return "", "", "", fmt.Errorf("scheme %q is not supported: use http:// or https://, or just the host name", h[:i])
		}
		h = h[i+3:]
	}
	if strings.Contains(h, "://") {
		return "", "", "", fmt.Errorf("%q has two schemes: use one http:// or https://, or just the host name", host)
	}
	if strings.Count(h, ":") > 1 && !strings.HasPrefix(h, "[") && net.ParseIP(h) != nil { // a bare IPv6 address
		return scheme, h, "", nil
	}
	u, perr := url.Parse("//" + h)
	if perr != nil || u.Hostname() == "" {
		return "", "", "", fmt.Errorf("%q is not a host name (want host, host:port, or http(s)://host[:port])", host)
	}
	if u.User != nil {
		return "", "", "", errors.New("must not contain credentials: use the user and password fields")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Hostname(), "://") {
		return "", "", "", fmt.Errorf("%q must be a host name only, without a path", host)
	}
	return scheme, u.Hostname(), u.Port(), nil
}

func (c Connection) EffectivePort() int {
	if c.Port != 0 {
		return c.Port
	}
	return DefaultPort(c.Driver)
}

// ErrPasswordAsk means the password must be prompted for (password = "ask").
var ErrPasswordAsk = errors.New("password: ask")

// ResolvePassword returns the literal password, or the stdout of password_cmd.
func (c Connection) ResolvePassword(ctx context.Context) (string, error) {
	if c.PasswordCmd == "" {
		if c.Password == PasswordAsk {
			return "", ErrPasswordAsk
		}
		return c.Password, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv, err := splitCommand(c.PasswordCmd)
	if err != nil {
		return "", fmt.Errorf("password_cmd for %q: %w", c.Name, err)
	}
	// Run the program directly: a shared connections.toml must not be able to run shell code.
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("password_cmd for %q failed: %w", c.Name, err)
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

// splitCommand splits a command line into argv with single and double quotes, no shell.
func splitCommand(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	in, quote := false, rune(0)
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if in {
				argv = append(argv, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced quote in %q", s)
	}
	if in {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return argv, nil
}

func ConnectionsPath() string { return filepath.Join(ConfigDir(), "connections.toml") }

func LoadConnections(path string) ([]Connection, error) { return loadConnections(path, true) }

// loadConnections: fix=false leaves permissions alone (shared source files are not ours to chmod).
func loadConnections(path string, fix bool) ([]Connection, error) {
	var f connectionsFile
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, os.ErrNotExist) {
		loadedMu.Lock()
		loaded[path] = map[string]bool{}
		loadedMu.Unlock()
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range md.Undecoded() { // a misspelled ssl_mode or safe_mode must not vanish silently
		if len(k) >= 3 && k[2] == "options" {
			continue
		}
		return nil, fmt.Errorf("%s: unknown setting %q", path, k.String())
	}
	// The file holds plaintext passwords: it is only ever read with owner-only permissions.
	if st, err := os.Stat(path); fix && err == nil && st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			if !chmodTolerable(path, st.Mode().Perm(), err) {
				return nil, fmt.Errorf("%s is readable by other users and could not be fixed: %w", path, err)
			}
			fmt.Fprintf(os.Stderr, "webtuidb: warning: could not chmod %s to 600 (%v); continuing\n", path, err)
		}
	}
	seen := map[string]bool{}
	for i := range f.Connection {
		c := &f.Connection[i]
		if !slices.Contains(Drivers, c.Driver) && strings.TrimSpace(c.Name) != "" {
			// A driver that no longer exists (sqlite, duckdb, ...) must not make the whole file unloadable.
			probe := *c
			probe.Driver = Postgres
			if err := probe.Validate(); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			c.Unusable = fmt.Sprintf("driver %q is not supported by this build", c.Driver)
			if seen[c.Name] {
				return nil, fmt.Errorf("%s: duplicate connection name %q", path, c.Name)
			}
			seen[c.Name] = true
			continue
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("%s: duplicate connection name %q", path, c.Name)
		}
		seen[c.Name] = true
	}
	names := map[string]bool{}
	for _, c := range f.Connection {
		names[c.Name] = true
	}
	loadedMu.Lock()
	loaded[path] = names
	loadedMu.Unlock()
	return f.Connection, nil
}

var (
	loadedMu sync.Mutex
	loaded   = map[string]map[string]bool{} // path -> connection names this process last loaded/saved
)

// SaveConnections writes atomically with 0600 perms (passwords are stored in plain text).
func SaveConnections(path string, conns []Connection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	unlock, err := LockFile(path)
	if err != nil {
		return err
	}
	defer unlock()
	// Keep connections another process added since this one loaded/saved (names we never saw);
	// names we knew and no longer have are deliberate deletions.
	loadedMu.Lock()
	known, hadSnapshot := loaded[path]
	loadedMu.Unlock()
	if hadSnapshot {
		var disk connectionsFile
		if _, err := toml.DecodeFile(path, &disk); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s changed on disk and is not valid TOML, refusing to overwrite it: %w", path, err)
			}
		} else {
			have := map[string]bool{}
			for _, c := range conns {
				have[c.Name] = true
			}
			for _, c := range disk.Connection {
				if !have[c.Name] && !known[c.Name] {
					conns = append(conns[:len(conns):len(conns)], c)
				}
			}
		}
	}
	names := map[string]bool{}
	for _, c := range conns {
		names[c.Name] = true
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(connectionsFile{Connection: conns}); err != nil {
		return err
	}
	if err := WriteFileAtomic(path, buf.Bytes()); err != nil {
		return err // the snapshot stays: nothing was written, so nothing is "known" yet
	}
	loadedMu.Lock()
	loaded[path] = names
	loadedMu.Unlock()
	return nil
}

// WriteFileAtomic writes data to a temp file in the same directory, fsyncs it, renames it over
// path and fsyncs the directory, so a crash leaves either the old or the new file, never an empty one.
func WriteFileAtomic(path string, data []byte) error {
	if path == "" {
		return errors.New("WriteFileAtomic: empty path")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close() // error paths must not leak the handle; the explicit Close below makes this a no-op
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

var urlSchemes = map[string]string{
	"postgres": Postgres, "postgresql": Postgres, "mysql": MySQL, "mariadb": MariaDB,
	"redshift": Redshift, "greenplum": Greenplum, "vertica": Vertica, "singlestore": SingleStore,
	"clickhouse": ClickHouse, "cassandra": Cassandra, "dynamodb": DynamoDB,
	"redis": Redis, "rediss": Redis, "mongodb": Mongo,
}

// SSLModes are the values of Connection.SSLMode.
var SSLModes = []string{"disable", "prefer", "require", "verify-ca", "verify-full", "verify-identity"}

// sslFromQuery reads TLS settings from a URL query in webtuidb's own spelling (sslmode=) and the
// engines' native ones, so a URL pasted from another tool does not silently connect in plaintext.
// It returns the mode and the query keys it consumed.
func sslFromQuery(driver string, q url.Values) (string, []string, error) {
	norm := func(v string) string { return strings.ReplaceAll(strings.ToLower(v), "_", "-") }
	// flag: true/false spellings; anything else is an error rather than a silent downgrade to plaintext.
	flag := func(k string) (string, error) {
		switch norm(q.Get(k)) {
		case "true", "1", "yes":
			return "require", nil
		case "false", "0", "no":
			return "disable", nil
		}
		return "", fmt.Errorf("unknown %s value %q (want true|false)", k, q.Get(k))
	}
	if v := q.Get("sslmode"); v != "" {
		return strings.ToLower(v), []string{"sslmode"}, nil
	}
	if v := q.Get("tLSMode"); v != "" { // TablePlus's spelling
		return strings.ToLower(v), []string{"tLSMode"}, nil
	}
	switch driver {
	case MySQL, MariaDB:
		if v := q.Get("ssl-mode"); v != "" {
			m, ok := map[string]string{"disabled": "disable", "preferred": "prefer", "required": "require", "verify-ca": "verify-ca", "verify-identity": "verify-identity"}[norm(v)]
			if !ok {
				return "", nil, fmt.Errorf("unknown ssl-mode %q (want %s)", v, strings.Join(SSLModes, "|"))
			}
			return m, []string{"ssl-mode"}, nil
		}
		if v := q.Get("tls"); v != "" {
			switch norm(v) {
			case "skip-verify":
				return "require", []string{"tls"}, nil
			case "preferred":
				return "prefer", []string{"tls"}, nil
			}
			m, err := flag("tls")
			return m, []string{"tls"}, err
		}
	case ClickHouse:
		if q.Get("secure") != "" {
			m, err := flag("secure")
			return m, []string{"secure"}, err
		}
	case Mongo:
		for _, k := range []string{"tls", "ssl"} {
			if q.Get(k) != "" {
				m, err := flag(k)
				return m, []string{k}, err
			}
		}
	}
	return "", nil, nil
}

// ParseURL imports driver://user:pass@host:port/db?name=..&env=..&safeModeLevel=..; the ssh and
// ssl/tag options use TablePlus's query names where they map (name, env, statusColor).
func ParseURL(raw string) (Connection, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse errors quote the whole URL, password included, so do not wrap err.
		return Connection{}, fmt.Errorf("bad URL (not shown: it may contain a password; percent-encode special characters such as %% @ in it)")
	}
	viaSSH := false
	if base, ok := strings.CutSuffix(strings.ToLower(u.Scheme), "+ssh"); ok { // postgresql+ssh://: tunnel in the query
		u.Scheme, viaSSH = base, true
	}
	if strings.EqualFold(u.Scheme, "mongodb+srv") {
		return Connection{}, fmt.Errorf("mongodb+srv:// is not supported (DNS seed lists); use mongodb://host:port")
	}
	driver, ok := urlSchemes[strings.ToLower(u.Scheme)]
	if !ok {
		return Connection{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	c := Connection{Driver: driver, Host: u.Hostname(), Database: strings.TrimPrefix(u.Path, "/")}
	if FileBased(driver) { // sqlite:///abs/x.db and sqlite://rel/x.db: the whole thing after // is the path
		c.Host, c.Database = "", u.Host+u.Path
	}
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
	mode, consumed, err := sslFromQuery(driver, q)
	if err != nil {
		return c, err
	}
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
	if q.Get("password") == PasswordAsk {
		c.Password = PasswordAsk
	}
	if viaSSH {
		if FileBased(driver) || q.Get("ssh_host") == "" {
			return c, fmt.Errorf("a +ssh URL needs ssh_host (and a network driver)")
		}
		c.SSH = &SSH{Host: q.Get("ssh_host"), User: q.Get("ssh_user"), Key: q.Get("ssh_key")}
		if p := q.Get("ssh_port"); p != "" {
			if c.SSH.Port, err = strconv.Atoi(p); err != nil {
				return c, fmt.Errorf("bad ssh_port %q", p)
			}
		}
		if v := strings.ToLower(q.Get("usePrivateKey")); v == "false" || v == "0" { // password auth: the key is not used
			c.SSH.Key = ""
		}
		consumed = append(consumed, "ssh_host", "ssh_user", "ssh_key", "ssh_port", "usePrivateKey")
	}
	c.DriverVersion = q.Get("driverVersion")
	c.LazyLoad = q.Get("lazyload") == "true" || q.Get("lazyload") == "1"
	if t := (OpenTarget{q.Get("schema"), q.Get("table"), q.Get("column"), q.Get("operation"), q.Get("value"), q.Get("condition")}); t.Table != "" {
		c.Open = &t
	}
	for k, v := range q {
		switch k {
		case "name", "env", "statusColor", "driverVersion", "lazyload", "schema", "table", "column", "operation", "value", "condition":
		default:
			// a ?password= other than "ask" is never used, so it is not kept; ssh settings that were not consumed (ssh_password,
			// sshPassword...) are dropped, never stored in Options.
			// shortcut: ssh_key from a pasted URL is kept as given (a server path the user chose); upgrade: confine it to the home dir.
			if contains(consumed, k) || k == "password" || strings.HasPrefix(strings.ToLower(k), "ssh") {

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
		if FileBased(driver) {
			c.Name = filepath.Base(c.Database)
		} else if c.Database != "" {
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
	host := c.Host
	if strings.Contains(host, ":") { // IPv6 literal
		host = "[" + host + "]"
	}
	if c.SSH != nil && c.SSH.Host != "" {
		scheme += "+ssh"
	}
	u := url.URL{Scheme: scheme, Host: host, Path: "/" + c.Database}
	if c.Port != 0 {
		u.Host = fmt.Sprintf("%s:%d", host, c.Port)
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
		q.Set("sslmode", c.SSLMode) // webtuidb's own spelling; ParseURL also reads the native ones
	}
	if c.SSH != nil && c.SSH.Host != "" {
		q.Set("ssh_host", c.SSH.Host)
		q.Set("ssh_user", c.SSH.User)
		if c.SSH.Port != 0 {
			q.Set("ssh_port", strconv.Itoa(c.SSH.Port))
		}
		if c.SSH.Key != "" {
			q.Set("ssh_key", c.SSH.Key)
			q.Set("usePrivateKey", "true")
		}
	}
	q.Set("name", c.Name)
	if c.DriverVersion != "" {
		q.Set("driverVersion", c.DriverVersion)
	}
	if c.LazyLoad {
		q.Set("lazyload", "true")
	}
	if c.Tag != "" {
		q.Set("env", c.Tag)
	}
	if c.Color != "" {
		q.Set("statusColor", strings.TrimPrefix(c.Color, "#"))
	}
	u.RawQuery = q.Encode()
	if FileBased(c.Driver) { // no host: the path follows // as typed
		return scheme + "://" + (&url.URL{Path: c.Database}).EscapedPath() + "?" + u.RawQuery
	}
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

// chmodTolerable: a failed chmod (not the owner, read-only mount: typical for a bind mount in a non-root container) is
// survivable when other users cannot read the file anyway, or when the directory cannot be written so nobody else can swap it.
func chmodTolerable(path string, perm os.FileMode, err error) bool {
	if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EROFS) {
		return false
	}
	if perm&0o044 == 0 {
		return true
	}
	f, cerr := os.CreateTemp(filepath.Dir(path), ".wtdb-probe-*")
	if cerr != nil {
		return true // directory not writable
	}
	f.Close()
	os.Remove(f.Name())
	return false
}
