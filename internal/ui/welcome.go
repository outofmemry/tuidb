package ui

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/keymap"
	"webtuidb/internal/safemode"
)

// welcome is the connection list shown at startup.
type welcome struct {
	a         *App
	sel, top  int
	search    lineEdit
	searching bool
	engine    int // 0 = all engines, otherwise 1+index into config.Drivers; filters the list and pre-fills n
	tileTop   int // first visible tile in the engine strip
}

// engineName is the display name on an engine tile; engineArt its four-line, eight-cell logo.
var engineName = map[string]string{
	config.Postgres: "Postgres", config.MySQL: "MySQL", config.MariaDB: "MariaDB", config.ClickHouse: "ClickHouse", config.Cassandra: "Cassandra",
	config.DynamoDB: "DynamoDB", config.Redis: "Redis", config.Mongo: "MongoDB",
	config.Redshift: "Redshift", config.Greenplum: "Greenplum", config.Vertica: "Vertica", config.SingleStore: "SingleStore",
}

var engineArt = map[string][]string{
	"": { // all engines: a database cylinder
		`  .---.  `,
		` (_____) `,
		` |     | `,
		` ` + "`" + `-----' `},
	config.Postgres: { // elephant
		` ,--.__  `,
		`( o   _\ `,
		` \_  (_) `,
		`  ` + "`" + `-'    `},
	config.MySQL: { // dolphin
		`    __   `,
		` .-'  ` + "`" + `. `,
		`(_,-._,' `,
		`   ` + "`" + `-'   `},
	config.MariaDB: { // seal
		`  __     `,
		` (  \__  `,
		`  \__  ) `,
		`    ` + "`" + `-'  `},
	config.Cockroach: { // roach
		` \_|_/   `,
		`-( o )-  `,
		`-(   )-  `,
		` /` + "`" + `|'\   `},
	config.SQLite: { // feather
		`    /|   `,
		`   / |   `,
		`  /  |   `,
		` /___|   `},
	config.DuckDB: { // duck
		`   __    `,
		` <(o )__ `,
		`  ( ._>  `,
		`   ` + "`" + `-'   `},
	config.ClickHouse: { // bars
		` _ _ _ _ `,
		`| | | | |`,
		`| | | | |`,
		`|_|_|_|_|`},
	config.Cassandra: { // eye
		`  .---.  `,
		` ( (@) ) `,
		`  ` + "`" + `---'  `,
		`   ~~~   `},
	config.DynamoDB: { // table
		` .-----. `,
		` |-----| `,
		` |-----| `,
		` ` + "`" + `-----' `},
	config.D1: { // cloud
		`   .--.  `,
		` .(    ) `,
		`(  D1   )`,
		` ` + "`" + `-----' `},
	config.Redis: { // stacked boxes
		`  .---.  `,
		` /_____\ `,
		` \_____/ `,
		`  \___/  `},
	config.Redshift: { // box with arrows
		` .-----. `,
		` |>>>>>| `,
		` |>>>>>| `,
		` ` + "`" + `-----' `},
	config.Greenplum: { // plum
		`   ,/    `,
		`  .--.   `,
		` (    )  `,
		`  ` + "`" + `--'   `},
	config.Vertica: { // V
		` \     / `,
		`  \   /  `,
		`   \ /   `,
		`    v    `},
	config.SingleStore: { // one store
		`  .---.  `,
		` | 1 1 | `,
		` | 1 1 | `,
		`  ` + "`" + `---'  `},
	config.Mongo: { // leaf
		`    ,    `,
		`   /|\   `,
		`  ( | )  `,
		`   \|/   `},
}

// trimArt cuts the blank columns shared by all lines of a logo so center can place its ink, not its padding.
func trimArt(art []string) []string {
	l, r := 9, 9
	for _, s := range art {
		l = min(l, len(s)-len(strings.TrimLeft(s, " ")))
		r = min(r, len(s)-len(strings.TrimRight(s, " ")))
	}
	out := make([]string, len(art))
	for i, s := range art {
		out[i] = s[l : len(s)-r]
	}
	return out
}

func center(s string, w int) string {
	pad := max(0, w-width(s))
	return strings.Repeat(" ", pad/2) + s + strings.Repeat(" ", pad-pad/2)
}

// driver returns the selected engine, or "" for all.
func (w *welcome) driver() string {
	if w.engine == 0 {
		return ""
	}
	return config.Drivers[w.engine-1]
}

func newWelcome(a *App) *welcome {
	w := &welcome{a: a}
	w.move(0)
	return w
}

type connRow struct {
	header string // group header when non-empty
	conn   config.Connection
	idx    int // index in a.d.Conns
}

// rows groups connections by Group path ("A/B"), filtered by the search text.
func (w *welcome) rows() []connRow {
	conns := w.a.d.Conns
	order := make([]int, 0, len(conns))
	for i, c := range conns {
		if d := w.driver(); d != "" && c.Driver != d {
			continue
		}
		if w.search.Value() == "" {
			order = append(order, i)
			continue
		}
		if _, ok := matchName(w.search.Value(), c.Name+" "+c.Host+" "+c.Tag, true); ok {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := conns[order[x]], conns[order[y]]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if ka, kb := w.sortKey(a), w.sortKey(b); ka != kb {
			return ka < kb
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	var out []connRow
	last := "\x00"
	for _, i := range order {
		c := conns[i]
		if c.Group != last {
			if c.Group != "" {
				out = append(out, connRow{header: c.Group})
			}
			last = c.Group
		}
		out = append(out, connRow{conn: c, idx: i})
	}
	return out
}

func (w *welcome) current() (connRow, bool) {
	rows := w.rows()
	for i := w.sel; i >= 0 && i < len(rows); {
		if rows[i].header == "" {
			return rows[i], true
		}
		i++
	}
	return connRow{}, false
}

// selectConn puts the cursor on the row of a.d.Conns[idx] (or the nearest connection when it is hidden).
func (w *welcome) selectConn(idx int) {
	for i, r := range w.rows() {
		if r.header == "" && r.idx == idx {
			w.sel = i
			return
		}
	}
	w.move(0)
}

// move steps over group headers; d==0 snaps onto the nearest connection at or after sel.
func (w *welcome) move(d int) {
	rows := w.rows()
	if d == 0 {
		for i := max(0, w.sel); i < len(rows); i++ {
			if rows[i].header == "" {
				w.sel = i
				return
			}
		}
		return
	}
	for i := w.sel + d; i >= 0 && i < len(rows); i += d {
		if rows[i].header == "" {
			w.sel = i
			return
		}
	}
}

func (w *welcome) input(msg tea.Msg) tea.Cmd {
	if w.searching {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.Keystroke() {
			case "esc":
				cur, ok := w.current() // sel indexes the filtered list: keep the same connection selected
				w.searching = false
				w.search.Set("")
				if ok {
					w.selectConn(cur.idx)
				}
				return nil
			case "enter":
				w.searching = false
				return nil
			case "down":
				w.move(1)
				return nil
			case "up":
				w.move(-1)
				return nil
			}
		}
		if w.search.Handle(msg) {
			w.sel = 0
			w.move(0)
		}
		return nil
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch keymap.FromMsg(k) {
	case "down", "j":
		w.move(1)
	case "up", "k":
		w.move(-1)
	case "right", "l":
		w.pick(1)
	case "left", "h":
		w.pick(-1)
	case "enter":
		if r, ok := w.current(); ok {
			return w.a.openConnection(r.conn)
		}
	case "/":
		w.searching = true
	case "esc":
		if w.a.cancelConnecting() > 0 {
			return w.a.say("cancelling…")
		}
	case "n":
		return w.action("new_connection")
	case "e":
		return w.edit()
	case "c":
		return w.duplicate()
	case "d", "delete":
		return w.remove()
	case "i":
		return w.importURL()
	case "x":
		return w.exportConns()
	case "I":
		return w.importConns()
	case "g":
		return w.groupMenu()
	case "m":
		return w.menu()
	case "s":
		return w.pickSort()
	case "u": // the clipboard is shared and often synced: no password unless asked for with U
		if r, ok := w.current(); ok {
			c := r.conn
			c.Password = ""
			return tea.Batch(setClip(c.FormatURL()), w.a.say("copied %s as URL without the password (U includes it)", c.Name))
		}
	case "U":
		if r, ok := w.current(); ok {
			c := r.conn
			w.a.push(&confirmDlg{title: "Copy the URL with the password to the clipboard?", lines: []string{"Clipboard managers and synced clipboards keep what is copied."}, yes: "Copy", danger: true,
				onYes: func() tea.Cmd {
					return tea.Batch(setClip(c.FormatURL()), w.a.say("copied %s with its password", c.Name))
				}})
		}
	case "tab":
		if len(w.a.wss) > 0 {
			w.a.inWS = true
		}
	}
	return nil
}

// pick moves the engine cursor and snaps the list back to its top.
func (w *welcome) pick(d int) {
	w.engine = clamp(w.engine+d, 0, len(config.Drivers))
	w.sel, w.top = 0, 0
	w.move(0)
}

func (w *welcome) action(action string) tea.Cmd {
	switch action {
	case "new_connection":
		return w.form(config.Connection{Driver: orDefault(w.driver(), config.Postgres)}, -1)
	case "open_database", "new_query_tab", "new_tab":
		return w.a.say("connect first: Enter on a connection")
	}
	return nil
}

// readOnlySource refuses to change a connection that comes from a [[source]] folder.
func (w *welcome) readOnlySource(c config.Connection) (tea.Cmd, bool) {
	if c.Source == "" {
		return nil, false
	}
	return w.a.say("%s comes from %s: edit that file (duplicate makes a local copy)", c.Name, c.Source), true
}

func (w *welcome) edit() tea.Cmd {
	if r, ok := w.current(); ok {
		if cmd, ro := w.readOnlySource(r.conn); ro {
			return cmd
		}
		return w.form(r.conn, r.idx)
	}
	return nil
}

func (w *welcome) duplicate() tea.Cmd {
	r, ok := w.current()
	if !ok {
		return nil
	}
	c := r.conn
	c.Source = "" // a copy of a source connection is a local, editable one
	c.Name += " copy"
	for _, o := range w.a.d.Conns {
		if o.Name == c.Name {
			c.Name += "2"
		}
	}
	w.a.d.Conns = append(w.a.d.Conns, c)
	if err := w.a.saveConns(); err != nil {
		return w.a.fail(err)
	}
	return w.a.say("duplicated %s", r.conn.Name)
}

func (w *welcome) remove() tea.Cmd {
	r, ok := w.current()
	if !ok {
		return nil
	}
	if cmd, ro := w.readOnlySource(r.conn); ro {
		return cmd
	}
	w.a.push(&confirmDlg{title: "Delete connection " + r.conn.Name + "?", lines: []string{"This removes it from connections.toml. The database itself is untouched."}, yes: "Delete", danger: true,
		onYes: func() tea.Cmd {
			w.a.d.Conns = append(w.a.d.Conns[:r.idx:r.idx], w.a.d.Conns[r.idx+1:]...)
			w.sel = 0
			w.move(0)
			if err := w.a.saveConns(); err != nil {
				return w.a.fail(err)
			}
			return w.a.say("deleted %s", r.conn.Name)
		}})
	return nil
}

func (w *welcome) importURL() tea.Cmd {
	w.a.push(newForm("Import connection from URL", []formField{
		{Key: "url", Label: "URL", Kind: fText},
		{Key: "hint", Label: "", Kind: fLabel, Value: "postgresql:// mysql:// clickhouse:// redis:// mongodb://  (?name=..&env=..)"},
	}, func(v map[string]string) tea.Cmd {
		c, err := config.ParseURL(strings.TrimSpace(v["url"]))
		if err != nil {
			return w.a.fail(err)
		}
		return w.form(c, -1)
	}))
	return nil
}

var sslModes = append([]string{""}, config.SSLModes...)

// form opens the create/edit dialog. idx<0 creates a new connection.
func (w *welcome) form(c config.Connection, idx int) tea.Cmd {
	ssh := config.SSH{Port: 22}
	if c.SSH != nil {
		ssh = *c.SSH
	}
	sshOn := "false"
	if c.SSH != nil && c.SSH.Host != "" {
		sshOn = "true"
	}
	tags := append([]string{""}, w.a.tagNames()...)
	if c.Tag != "" && !contains(tags, c.Tag) {
		tags = append(tags, c.Tag)
	}
	safe := make([]string, len(safemode.All))
	for i, l := range safemode.All {
		safe[i] = string(l)
	}
	if c.SafeMode == "" {
		c.SafeMode = w.a.d.Cfg.General.DefaultSafeMode
	}
	port := ""
	if c.Port != 0 {
		port = strconv.Itoa(c.Port)
	}
	server := func(v map[string]string) bool { return !config.FileBased(v["driver"]) } // file engines: only Database (the path)
	showSSH := func(v map[string]string) bool { return server(v) && v["ssh"] == "true" }
	showSSL := func(v map[string]string) bool { return v["ssl_mode"] != "" && v["ssl_mode"] != "disable" }
	title := "New connection"
	if idx >= 0 {
		title = "Edit connection"
	}
	f := newForm(title, []formField{
		{Kind: fHeading, Value: "Server"},
		{Key: "name", Label: "Name", Kind: fText, Value: c.Name},
		{Key: "driver", Label: "Driver", Kind: fChoice, Value: c.Driver, Options: config.Drivers},
		{Key: "host", Label: "Host", Kind: fText, Value: orDefault(c.Host, "127.0.0.1"), Show: server},
		{Key: "port", Label: "Port", Kind: fText, Value: port, Show: server},
		{Key: "user", Label: "User", Kind: fText, Value: c.User, Show: server},
		{Key: "password", Label: "Password", Kind: fSecret, Value: c.Password, Show: server},
		{Key: "password_cmd", Label: "Password cmd", Kind: fText, Value: c.PasswordCmd, Show: server},
		{Key: "database", Label: "Database", Kind: fText, Value: c.Database},
		{Kind: fHeading, Value: "Workspace"},
		{Key: "tag", Label: "Tag", Kind: fChoice, Value: c.Tag, Options: tags},
		{Key: "color", Label: "Status color", Kind: fText, Value: c.Color},
		{Key: "safe_mode", Label: "Safe mode", Kind: fChoice, Value: c.SafeMode, Options: safe},
		{Key: "group", Label: "Group", Kind: fText, Value: c.Group},
		{Kind: fHeading, Value: "Transport"},
		{Key: "encoding", Label: "Encoding", Kind: fText, Value: c.Encoding, Hint: "server default (UTF8, latin1…)", Show: server},
		{Key: "ssl_mode", Label: "SSL mode", Kind: fChoice, Value: c.SSLMode, Options: sslModes},
		{Key: "ssl_ca", Label: "  CA cert", Kind: fText, Value: c.SSLCA, Show: showSSL},
		{Key: "ssl_cert", Label: "  Client cert", Kind: fText, Value: c.SSLCert, Show: showSSL},
		{Key: "ssl_key", Label: "  Client key", Kind: fText, Value: c.SSLKey, Show: showSSL},
		{Key: "socks_proxy", Label: "SOCKS5 proxy", Kind: fText, Value: c.SocksProxy, Hint: "host:port", Show: server},
		{Key: "iam_auth", Label: "AWS IAM auth", Kind: fBool, Value: strconv.FormatBool(c.IAMAuth), Show: func(v map[string]string) bool {
			d := v["driver"]
			return server(v) && (d == "" || d == config.Postgres || d == config.MySQL || d == config.MariaDB)
		}},
		{Key: "read_only", Label: "Read only", Kind: fBool, Value: strconv.FormatBool(c.ReadOnly)},
		{Key: "ssh", Label: "Over SSH", Kind: fBool, Value: sshOn, Show: server},
		{Key: "ssh_host", Label: "  SSH host", Kind: fText, Value: ssh.Host, Show: showSSH},
		{Key: "ssh_port", Label: "  SSH port", Kind: fText, Value: strconv.Itoa(orInt(ssh.Port, 22)), Show: showSSH},
		{Key: "ssh_user", Label: "  SSH user", Kind: fText, Value: ssh.User, Show: showSSH},
		{Key: "ssh_key", Label: "  SSH key file", Kind: fText, Value: ssh.Key, Show: showSSH},
		{Key: "ssh_password", Label: "  SSH password", Kind: fSecret, Value: ssh.Password, Show: showSSH},
		{Kind: fHeading, Value: "After connecting"},
		{Key: "options", Label: "Options", Kind: fText, Value: optionsText(c.Options)},
		{Key: "options_note", Label: "", Kind: fLabel, Value: "driver extras as key=value, separated by ' ;; ' (DynamoDB: region=..., Cassandra: keyspace=...)"},
		{Key: "bootstrap", Label: "Bootstrap SQL", Kind: fText, Value: strings.Join(c.Bootstrap, " ;; ")},
		{Key: "note", Label: "", Kind: fLabel, Value: "statements run on every new connection, separated by ' ;; '"},
	}, func(v map[string]string) tea.Cmd { return w.save(v, idx) })
	f.validate = func(v map[string]string) error {
		_, err := w.build(v, idx)
		return err
	}
	f.submit = "Enter: save"
	f.hotHint = "Ctrl+t: test   Ctrl+o: password cmd preset"
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{"ctrl+t": func(v map[string]string) tea.Cmd {
		c, err := w.build(v, idx)
		if err != nil {
			return w.a.fail(err)
		}
		return w.testConn(c)
	}, "ctrl+o": func(v map[string]string) tea.Cmd { return w.pickPasswordCmd(f, v["name"]) }}
	f.onChange = func(key string, v map[string]string) { // swap the default port when the driver changes
		if key != "driver" {
			return
		}
		for _, fl := range f.fields {
			if fl.Key == "port" {
				old := fl.edit.Value()
				if old == "" || v["driver"] != c.Driver && (old == strconv.Itoa(config.DefaultPort(c.Driver)) || isDefaultPort(old)) { // only when the driver really changed
					fl.edit.Set(portText(config.DefaultPort(v["driver"])))
				}
			}
		}
		c.Driver = v["driver"]
	}
	if port == "" {
		f.onChange("driver", f.values())
	}
	opened := f.values()
	f.onCancel = func() tea.Cmd { // Esc: edits are not thrown away without asking
		if maps.Equal(f.values(), opened) {
			return nil
		}
		w.a.push(&confirmDlg{title: "Discard changes to this connection?", yes: "Discard", danger: true,
			onYes: func() tea.Cmd { return nil }, onNo: func() tea.Cmd { w.a.push(f); return nil }})
		return nil
	}
	w.a.push(f)
	return nil
}

func isDefaultPort(s string) bool {
	for _, d := range config.Drivers {
		if s == strconv.Itoa(config.DefaultPort(d)) {
			return true
		}
	}
	return false
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func orInt(n, d int) int {
	if n == 0 {
		return d
	}
	return n
}

func (w *welcome) build(v map[string]string, idx int) (config.Connection, error) {
	c := config.Connection{
		Name: strings.TrimSpace(v["name"]), Driver: v["driver"], Host: strings.TrimSpace(v["host"]), User: v["user"], Password: v["password"],
		PasswordCmd: v["password_cmd"], Database: v["database"], Tag: v["tag"], Color: v["color"], SafeMode: v["safe_mode"], Group: strings.Trim(v["group"], "/ "),
		SSLMode: v["ssl_mode"], SSLCA: v["ssl_ca"], SSLCert: v["ssl_cert"], SSLKey: v["ssl_key"],
		Encoding: strings.TrimSpace(v["encoding"]), SocksProxy: strings.TrimSpace(v["socks_proxy"]), IAMAuth: v["iam_auth"] == "true", ReadOnly: v["read_only"] == "true",
	}
	var old config.Connection // settings the form has no field for survive an edit
	if idx >= 0 && idx < len(w.a.d.Conns) {
		old = w.a.d.Conns[idx]
		c.DriverVersion, c.LazyLoad = old.DriverVersion, old.LazyLoad
		c.Tools, c.Options, c.Source = old.Tools, old.Options, old.Source
	}
	if p := strings.TrimSpace(v["port"]); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("port must be 1-65535")
		}
		c.Port = n
	}
	if c.Color != "" && !strings.HasPrefix(c.Color, "#") {
		c.Color = "#" + c.Color
	}
	if _, err := safemode.Parse(c.SafeMode); err != nil {
		return c, err
	}
	for _, b := range strings.Split(v["bootstrap"], ";;") {
		if b = strings.TrimSpace(b); b != "" {
			c.Bootstrap = append(c.Bootstrap, b)
		}
	}
	if v["ssh"] == "true" {
		p, err := strconv.Atoi(strings.TrimSpace(v["ssh_port"]))
		if err != nil {
			return c, fmt.Errorf("ssh port must be a number")
		}
		if strings.TrimSpace(v["ssh_host"]) == "" {
			return c, fmt.Errorf("ssh host is required when Over SSH is on")
		}
		c.SSH = &config.SSH{Host: strings.TrimSpace(v["ssh_host"]), Port: p, User: v["ssh_user"], Key: v["ssh_key"], Password: v["ssh_password"]}
		if old.SSH != nil {
			c.SSH.ProxyJump = old.SSH.ProxyJump
		}
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	for i, o := range w.a.d.Conns {
		if o.Name == c.Name && i != idx {
			return c, fmt.Errorf("a connection named %q already exists", c.Name)
		}
	}
	for _, o := range strings.Split(v["options"], ";;") {
		if k, val, ok := strings.Cut(o, "="); ok && strings.TrimSpace(k) != "" {
			if c.Options == nil {
				c.Options = map[string]string{}
			}
			c.Options[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	if config.FileBased(c.Driver) { // hidden fields must not carry stale values from another driver
		c.Host, c.Port, c.User, c.Password, c.PasswordCmd, c.SSH = "", 0, "", "", "", nil
	}
	return c, nil
}

// optionsText renders Options as "k=v ;; k=v" in key order.
func optionsText(o map[string]string) string {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + o[k]
	}
	return strings.Join(parts, " ;; ")
}

func portText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func (w *welcome) save(v map[string]string, idx int) tea.Cmd {
	c, err := w.build(v, idx)
	if err != nil {
		return w.a.fail(err)
	}
	if idx >= 0 {
		w.a.d.Conns[idx] = c
	} else {
		w.a.d.Conns = append(w.a.d.Conns, c)
	}
	if err := w.a.saveConns(); err != nil {
		return w.a.fail(err)
	}
	return w.a.say("saved %s", c.Name)
}

func (w *welcome) view(th Theme, W, H int) string {
	n := len(w.a.d.Conns)
	rows := w.rows()
	byDriver := map[string]int{}
	for _, c := range w.a.d.Conns {
		byDriver[c.Driver]++
	}
	// ---- header: wordmark + tagline (a single line when the terminal is short)
	var head []string
	tag := []string{
		th.Title.Render("a database client for the terminal"),
		th.Dim.Render(fmt.Sprintf("%d engines · %d saved", len(config.Drivers), n)),
		"",
		th.Hint.Render("browse and edit tables, run queries, review the SQL before it is sent."),
		th.Hint.Render("every action is ") + th.chip(keymap.Display(w.a.d.Keys.Prefix)) + th.Hint.Render(" then a key; ") + th.chip(keymap.Display(w.a.d.Keys.Prefix)+" ?") + th.Hint.Render(" lists them."),
	}
	if H >= 26 {
		for i, l := range Logo {
			line := "  " + th.LogoStyle(i, len(Logo)).Render(l)
			if W >= logoW+50 && i < len(tag) {
				line += "   " + tag[i]
			}
			head = append(head, line)
		}
	} else {
		head = append(head, "  "+th.LogoStyle(0, 1).Render("webtuidb")+"  "+tag[0])
	}
	head = append(head, "")
	// ---- engine strip: one logo tile per driver; the cursor filters the list and pre-fills n
	if H-len(head) >= 17 { // strip (9 rows) + frame borders + the 6-row body floor; a short terminal drops it so the list stays visible
		const tileW = 13
		tiles := append([]string{""}, config.Drivers...)
		fitTiles := max(1, (W-6)/(tileW+1))
		w.tileTop = clamp(w.tileTop, max(0, w.engine-fitTiles+1), min(w.engine, max(0, len(tiles)-fitTiles)))
		tile := make([]string, 7)
		for i := w.tileTop; i < min(len(tiles), w.tileTop+fitTiles); i++ {
			d := tiles[i]
			name := "All"
			if d != "" {
				name = engineName[d]
			}
			edge, art := th.Border, th.DriverStyle(d)
			if d == "" {
				art = th.Accent
			}
			if i == w.engine {
				edge = th.Accent
			}
			label := center(name, tileW-2)
			if i == w.engine {
				label = th.Bold.Render(label)
			}
			tile[0] += " " + edge.Render("╭"+strings.Repeat("─", tileW-2)+"╮")
			for j, l := range trimArt(engineArt[d]) {
				tile[1+j] += " " + edge.Render("│") + art.Render(center(l, tileW-2)) + edge.Render("│")
			}
			tile[5] += " " + edge.Render("│") + label + edge.Render("│")
			tile[6] += " " + edge.Render("╰"+strings.Repeat("─", tileW-2)+"╯")
		}
		more := ""
		if w.tileTop > 0 {
			more += th.Dim.Render(fmt.Sprintf("  ‹ %d", w.tileTop))
		}
		if w.tileTop+fitTiles < len(tiles) {
			more += th.Dim.Render(fmt.Sprintf("  › %d more", len(tiles)-w.tileTop-fitTiles))
		}
		head = append(head, " "+th.Section.Render("Engines")+"  "+th.chips("←/→", "pick")+th.Hint.Render("  filters the list; ")+th.chip("n")+th.Hint.Render(" creates one")+more)
		head = append(head, tile...)
		head = append(head, "")
	}
	// ---- body: connections frame (+ quick start frame when wide enough)
	const lastW = 9
	last := w.a.d.State.LastConnected()
	bodyH := max(6, H-len(head)-2)
	sideW := 0
	if W >= 118 {
		sideW = 54
	}
	listW := W - sideW
	if sideW > 0 {
		listW--
	}
	addrW := clamp(listW-4-41-12-lastW, 4, 26) // host column shrinks first so the TAG badge (up to 12 cells) stays visible
	var list []string
	if w.searching || w.search.Value() != "" {
		list = append(list, th.Accent.Render(" / ")+w.search.View(listW-6, th, w.searching), "")
	}
	if len(rows) == 0 {
		what := "connections"
		if d := w.driver(); d != "" {
			what = engineName[d] + " connections"
		}
		list = append(list, "",
			"  "+th.Bold.Render("No "+what+" yet."),
			"",
			"  "+th.chips("n", "create one"),
			"  "+th.chips("i", "import a URL such as postgresql://user:pw@host/db"),
			"",
			"  "+th.Dim.Render("or start with one:  webtuidb postgresql://user:pw@host:5432/db"))
	} else {
		list = append(list, "   "+th.Section.Render(fit("NAME", 26)+fit("DRIVER", 12)+fit("HOST", addrW)+fit("LAST", lastW)+"TAG"))
	}
	listH := max(1, bodyH-2-len(list))
	w.sel = clamp(w.sel, 0, max(0, len(rows)-1))
	if w.sel < w.top {
		w.top = w.sel
	}
	if w.sel >= w.top+listH {
		w.top = w.sel - listH + 1
	}
	for i := w.top; i < min(len(rows), w.top+listH); i++ {
		r := rows[i]
		if r.header != "" {
			list = append(list, " "+th.Section.Render("▾ "+r.header))
			continue
		}
		c := r.conn
		tagS := ""
		if c.Tag != "" {
			tagS = th.TagStyle(c.Tag, w.a.tagColor(c)).Render(" " + strings.ToUpper(c.Tag) + " ")
		} else if c.Color != "" {
			tagS = th.TagStyle("", c.Color).Render(" ")
		}
		if c.ReadOnly {
			tagS += th.Dim.Render(" RO")
		}
		if c.Source != "" {
			tagS += th.Dim.Render(" source")
		}
		cursor := " "
		if i == w.sel {
			cursor = th.Accent.Render("▸")
		}
		ind := ""
		if c.Group != "" {
			ind = "  "
		}
		addr := c.Host
		if c.Port != 0 {
			addr += fmt.Sprintf(":%d", c.Port)
		}
		if c.SSH != nil && c.SSH.Host != "" {
			addr += " via ssh"
		}
		name := fit(ind+c.Name, 26)
		if i == w.sel {
			name = th.Bold.Render(name)
		}
		// a stripe in the driver color leads each row, so a mixed list scans by engine
		line := th.DriverStyle(c.Driver).Render("▎") + cursor + " " + name + th.DriverStyle(c.Driver).Render(fit(c.Driver, 12)) + th.Dim.Render(fit(addr, addrW)) + th.Dim.Render(fit(ago(last[c.Name]), lastW)) + tagS
		if i == w.sel {
			line = th.SelRow.Render(fit(line, listW-4))
		}
		list = append(list, line)
	}
	title := fmt.Sprintf("Connections (%d)", n)
	if d := w.driver(); d != "" {
		title = fmt.Sprintf("%s connections (%d of %d)", engineName[d], byDriver[d], n)
	}
	if len(w.a.wss) > 0 {
		title += fmt.Sprintf(" · %d open", len(w.a.wss))
	}
	listFrame := frame(th, title, th.chips("↵", "connect", "n", "new", "e", "edit", "/", "search"), strings.Join(list, "\n"), listW, bodyH, true)
	body := listFrame
	if sideW > 0 {
		k := func(ctx, action string) string { return boundKey(w.a.d.Keys, ctx, action) } // follows [keys] (T-P2-06)
		g := keymap.Global
		qs := []string{
			" " + th.Section.Render("Connections"),
			"  " + th.chips("↵", "connect to the highlighted one"),
			"  " + th.chips("n", "new") + "  " + th.chips("e", "edit") + "  " + th.chips("c", "duplicate") + "  " + th.chips("d", "delete"),
			"  " + th.chips("i", "import a URL") + "  " + th.chips("u", "copy as URL"),
			"  " + th.chips("g", "groups") + "  " + th.chips("s", "sort by"),
			"",
			" " + th.Section.Render("Once connected"),
			"  " + th.chips("↵", "open a table from the sidebar"),
			"  " + th.chips(k(g, "new_query_tab"), "SQL editor", k(g, "open_anything"), "find anything"),
			"  " + th.chips(k(keymap.Grid, "commit"), "commit", k(g, "preview"), "preview SQL"),
			"  " + th.chips(k(keymap.Grid, "filters"), "filter rows", k(g, "safe_mode"), "safe mode"),
			"",
			" " + th.Section.Render("Files"),
			"  " + th.Dim.Render("~/.config/webtuidb/connections.toml"),
			"  " + th.Dim.Render("~/.config/webtuidb/config.toml   keys and prefix"),
			"  " + th.Dim.Render("webtuidb -keys prints your bindings"),
		}
		side := frame(th, "Quick start", "", strings.Join(qs, "\n"), sideW, bodyH, false)
		body = lipgloss.JoinHorizontal(lipgloss.Top, listFrame, " ", side)
	}
	lines := append(head, strings.Split(body, "\n")...)
	for len(lines) < H {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = fit(lines[i], W)
	}
	return strings.Join(lines[:H], "\n")
}
