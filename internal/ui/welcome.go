package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tuidb/internal/config"
	"tuidb/internal/keymap"
	"tuidb/internal/safemode"
)

// welcome is the connection list shown at startup.
type welcome struct {
	a         *App
	sel, top  int
	search    lineEdit
	searching bool
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
				w.searching = false
				w.search.Set("")
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
	case "enter":
		if r, ok := w.current(); ok {
			return w.a.openConnection(r.conn)
		}
	case "/":
		w.searching = true
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
	case "u": // the clipboard is shared and often synced: no password unless asked for with U
		if r, ok := w.current(); ok {
			c := r.conn
			c.Password = ""
			return tea.Batch(tea.SetClipboard(c.FormatURL()), w.a.say("copied %s as URL without the password (U includes it)", c.Name))
		}
	case "U":
		if r, ok := w.current(); ok {
			c := r.conn
			w.a.push(&confirmDlg{title: "Copy the URL with the password to the clipboard?", lines: []string{"Clipboard managers and synced clipboards keep what is copied."}, yes: "Copy", danger: true,
				onYes: func() tea.Cmd {
					return tea.Batch(tea.SetClipboard(c.FormatURL()), w.a.say("copied %s with its password", c.Name))
				}})
		}
	case "tab":
		if len(w.a.wss) > 0 {
			w.a.inWS = true
		}
	}
	return nil
}

func (w *welcome) action(action string) tea.Cmd {
	switch action {
	case "new_connection":
		return w.form(config.Connection{Driver: config.Postgres}, -1)
	case "open_database", "new_query_tab", "new_tab":
		return w.a.say("connect first: Enter on a connection")
	}
	return nil
}

func (w *welcome) edit() tea.Cmd {
	if r, ok := w.current(); ok {
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
	tags := append([]string{""}, config.DefaultTags...)
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
	showSSH := func(v map[string]string) bool { return v["ssh"] == "true" }
	showSSL := func(v map[string]string) bool { return v["ssl_mode"] != "" && v["ssl_mode"] != "disable" }
	title := "New connection"
	if idx >= 0 {
		title = "Edit connection"
	}
	f := newForm(title, []formField{
		{Kind: fHeading, Value: "Server"},
		{Key: "name", Label: "Name", Kind: fText, Value: c.Name},
		{Key: "driver", Label: "Driver", Kind: fChoice, Value: c.Driver, Options: config.Drivers},
		{Key: "host", Label: "Host", Kind: fText, Value: orDefault(c.Host, "127.0.0.1")},
		{Key: "port", Label: "Port", Kind: fText, Value: port},
		{Key: "user", Label: "User", Kind: fText, Value: c.User},
		{Key: "password", Label: "Password", Kind: fSecret, Value: c.Password},
		{Key: "password_cmd", Label: "Password cmd", Kind: fText, Value: c.PasswordCmd},
		{Key: "database", Label: "Database", Kind: fText, Value: c.Database},
		{Kind: fHeading, Value: "Workspace"},
		{Key: "tag", Label: "Tag", Kind: fChoice, Value: c.Tag, Options: tags},
		{Key: "color", Label: "Status color", Kind: fText, Value: c.Color},
		{Key: "safe_mode", Label: "Safe mode", Kind: fChoice, Value: c.SafeMode, Options: safe},
		{Key: "group", Label: "Group", Kind: fText, Value: c.Group},
		{Kind: fHeading, Value: "Transport"},
		{Key: "ssl_mode", Label: "SSL mode", Kind: fChoice, Value: c.SSLMode, Options: sslModes},
		{Key: "ssl_ca", Label: "  CA cert", Kind: fText, Value: c.SSLCA, Show: showSSL},
		{Key: "ssl_cert", Label: "  Client cert", Kind: fText, Value: c.SSLCert, Show: showSSL},
		{Key: "ssl_key", Label: "  Client key", Kind: fText, Value: c.SSLKey, Show: showSSL},
		{Key: "ssh", Label: "Over SSH", Kind: fBool, Value: sshOn},
		{Key: "ssh_host", Label: "  SSH host", Kind: fText, Value: ssh.Host, Show: showSSH},
		{Key: "ssh_port", Label: "  SSH port", Kind: fText, Value: strconv.Itoa(orInt(ssh.Port, 22)), Show: showSSH},
		{Key: "ssh_user", Label: "  SSH user", Kind: fText, Value: ssh.User, Show: showSSH},
		{Key: "ssh_key", Label: "  SSH key file", Kind: fText, Value: ssh.Key, Show: showSSH},
		{Key: "ssh_password", Label: "  SSH password", Kind: fSecret, Value: ssh.Password, Show: showSSH},
		{Kind: fHeading, Value: "After connecting"},
		{Key: "bootstrap", Label: "Bootstrap SQL", Kind: fText, Value: strings.Join(c.Bootstrap, " ;; ")},
		{Key: "note", Label: "", Kind: fLabel, Value: "statements run on every new connection, separated by ' ;; '"},
	}, func(v map[string]string) tea.Cmd { return w.save(v, idx) })
	f.validate = func(v map[string]string) error {
		_, err := w.build(v, idx)
		return err
	}
	f.submit = "Enter: save"
	f.hotHint = "Ctrl+t: test"
	f.hotkeys = map[string]func(map[string]string) tea.Cmd{"ctrl+t": func(v map[string]string) tea.Cmd {
		c, err := w.build(v, idx)
		if err != nil {
			return w.a.fail(err)
		}
		connect := w.a.d.Connect
		return tea.Batch(w.a.say("testing %s…", c.Name), func() tea.Msg {
			o, err := connect(context.Background(), c)
			info := ""
			if o != nil {
				info = o.Sess.ServerInfo()
				o.Close()
			}
			return testedMsg{name: c.Name, info: info, err: err}
		})
	}}
	f.onChange = func(key string, v map[string]string) { // swap the default port when the driver changes
		if key != "driver" {
			return
		}
		for _, fl := range f.fields {
			if fl.Key == "port" {
				old := fl.edit.Value()
				if old == "" || old == strconv.Itoa(config.DefaultPort(c.Driver)) || isDefaultPort(old) {
					fl.edit.Set(strconv.Itoa(config.DefaultPort(v["driver"])))
				}
			}
		}
		c.Driver = v["driver"]
	}
	if port == "" {
		f.onChange("driver", f.values())
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
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	for i, o := range w.a.d.Conns {
		if o.Name == c.Name && i != idx {
			return c, fmt.Errorf("a connection named %q already exists", c.Name)
		}
	}
	// keep driver-specific options (authSource...) that the form does not expose
	if idx >= 0 {
		c.Options = w.a.d.Conns[idx].Options
	}
	return c, nil
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
	// ---- header: logo + what this is
	var head []string
	tag := []string{
		th.Title.Render("a database client for the terminal"),
		th.Dim.Render("PostgreSQL · MySQL · ClickHouse · Redis · MongoDB"),
		"",
		th.Hint.Render("browse and edit tables, run queries, review the SQL before it is sent."),
		th.Hint.Render("every action is ") + th.chip(keymap.Display(w.a.d.Keys.Prefix)) + th.Hint.Render(" then a key; ") + th.chip(keymap.Display(w.a.d.Keys.Prefix)+" ?") + th.Hint.Render(" lists them."),
	}
	for i, l := range Logo {
		line := "  " + th.LogoStyle(i, len(Logo)).Render(l)
		if W >= logoW+50 && i < len(tag) {
			line += "   " + tag[i]
		}
		head = append(head, line)
	}
	head = append(head, "")
	// ---- body: connections frame (+ quick start frame when wide enough)
	bodyH := max(6, H-len(head)-2)
	sideW := 0
	if W >= 118 {
		sideW = 54
	}
	listW := W - sideW
	if sideW > 0 {
		listW--
	}
	var list []string
	if w.searching || w.search.Value() != "" {
		list = append(list, th.Accent.Render(" / ")+w.search.View(listW-6, th, w.searching), "")
	}
	if n == 0 {
		list = append(list, "",
			"  "+th.Bold.Render("No connections yet."),
			"",
			"  "+th.chips("n", "create a connection"),
			"  "+th.chips("i", "import a URL such as postgresql://user:pw@host/db"),
			"",
			"  "+th.Dim.Render("or start with one:  tuidb postgresql://user:pw@host:5432/db"))
	} else {
		list = append(list, "   "+th.Section.Render(fit("NAME", 26)+fit("DRIVER", 12)+fit("HOST", 26)+"TAG"))
	}
	listW -= 0
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
			tagS = th.TagStyle(c.Tag, c.Color).Render(" " + strings.ToUpper(c.Tag) + " ")
		} else if c.Color != "" {
			tagS = th.TagStyle("", c.Color).Render(" ")
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
		line := th.DriverStyle(c.Driver).Render("▎") + cursor + " " + name + th.DriverStyle(c.Driver).Render(fit(c.Driver, 12)) + th.Dim.Render(fit(addr, 26)) + tagS
		if i == w.sel {
			line = th.SelRow.Render(fit(line, listW-4))
		}
		list = append(list, line)
	}
	title := fmt.Sprintf("Connections (%d)", n)
	if len(w.a.wss) > 0 {
		title += fmt.Sprintf(" · %d open", len(w.a.wss))
	}
	listFrame := frame(th, title, th.chips("↵", "connect", "n", "new", "e", "edit", "/", "search"), strings.Join(list, "\n"), listW, bodyH, true)
	body := listFrame
	if sideW > 0 {
		pre := keymap.Display(w.a.d.Keys.Prefix)
		qs := []string{
			" " + th.Section.Render("Connections"),
			"  " + th.chips("↵", "connect to the highlighted one"),
			"  " + th.chips("n", "new") + "  " + th.chips("e", "edit") + "  " + th.chips("c", "duplicate") + "  " + th.chips("d", "delete"),
			"  " + th.chips("i", "import a URL") + "  " + th.chips("u", "copy as URL"),
			"",
			" " + th.Section.Render("Once connected"),
			"  " + th.chips("↵", "open a table from the sidebar"),
			"  " + th.chips(pre+" e", "SQL editor") + "  " + th.chips(pre+" p", "find anything"),
			"  " + th.chips(pre+" s", "commit") + "  " + th.chips(pre+" P", "preview SQL"),
			"  " + th.chips(pre+" f", "filter rows") + "  " + th.chips(pre+" S", "safe mode"),
			"",
			" " + th.Section.Render("Files"),
			"  " + th.Dim.Render("~/.config/tuidb/connections.toml"),
			"  " + th.Dim.Render("~/.config/tuidb/config.toml   keys and prefix"),
			"  " + th.Dim.Render("tuidb -keys prints your bindings"),
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
