// webtuidb is a terminal TablePlus: connect to PostgreSQL, MySQL, MariaDB, Redshift, Greenplum, Vertica, SingleStore, ClickHouse, Cassandra, DynamoDB, Redis and MongoDB,
// browse and edit data, and run queries, with tmux-style prefix key bindings.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/crashlog"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
	"webtuidb/internal/mcp"
	"webtuidb/internal/plugin"
	"webtuidb/internal/store"
	"webtuidb/internal/ui"
	"webtuidb/internal/web"

	_ "webtuidb/internal/db/all"
	_ "webtuidb/internal/db/cassandra"
	_ "webtuidb/internal/db/clickhouse"
	_ "webtuidb/internal/db/dynamodb"
	_ "webtuidb/internal/db/mariadb"
	_ "webtuidb/internal/db/mongo"
	_ "webtuidb/internal/db/mysql"
	_ "webtuidb/internal/db/postgres"
	_ "webtuidb/internal/db/redis"
)

// Version is set at release time with -ldflags "-X main.Version=v1.2.3".
var Version = "dev"

//go:embed docs/webtuidb.1
var manPage string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "webtuidb:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", filepath.Join(config.ConfigDir(), "config.toml"), "path to config.toml")
	keys := flag.Bool("keys", false, "print the key bindings (after config overrides) and exit")
	theme := flag.Bool("theme", false, "print the resolved theme as [theme.colors] TOML (paste it into config.toml) and exit")
	frontend := flag.Bool("frontend", false, "serve the web UI instead of starting the terminal UI")
	listen := flag.String("listen", "127.0.0.1:7788", "with --frontend: address to listen on")
	openBrowser := flag.Bool("open", false, "with --frontend: open the browser")
	token := flag.String("token", "", "with --frontend: API token (default: random)")
	insecure := flag.Bool("insecure-listen", false, "with --frontend: allow a non-loopback --listen address (credentials go over the network)")
	mcpStdio := flag.Bool("mcp", false, "serve the MCP tools over stdio and exit")
	version := flag.Bool("version", false, "print the version and exit")
	sshLog := flag.String("ssh-log", "", "write an SSH tunnel debug log to this file")
	mcpConfig := flag.String("mcp-config", "", "print the MCP server entry for a client (claude-code | codex | cursor | vscode) and exit")
	completion := flag.String("completion", "", "print a shell completion script (bash | zsh | fish) and exit")
	man := flag.Bool("man", false, "print the man page (roff) and exit")
	listConns := flag.Bool("list-connections", false, "print the saved connection names, one per line (used by the completion scripts) and exit")
	changelogF := flag.Bool("changelog", false, "open the release notes in your browser (prints the URL) and exit")
	bugreport := flag.Bool("bugreport", false, "print version, OS, terminal, drivers in use and the config with secrets masked, and exit")
	checkUpd := flag.Bool("check-update", false, "ask GitHub for the latest release and say whether yours is older (never automatic) and exit")
	setPass := flag.Bool("set-passcode", false, "read a passcode twice and print the [security] passcode line for config.toml, then exit")
	themeInst := flag.String("theme-install", "", "install a theme from a git `url`[@ref] into the themes folder and exit")
	themeUpd := flag.Bool("theme-update", false, "git pull every installed theme and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: webtuidb [flags] [connection-name | URL]\n       webtuidb --frontend [--listen addr] [--open]\n\n  URL: postgresql:// mysql:// mariadb:// redshift:// greenplum:// vertica:// singlestore:// clickhouse:// cassandra:// dynamodb:// redis:// mongodb://\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if err := checkModes(map[string]bool{
		"frontend": *frontend, "mcp": *mcpStdio, "keys": *keys, "theme": *theme, "man": *man,
		"completion": *completion != "", "mcp-config": *mcpConfig != "", "list-connections": *listConns, "version": *version,
		"changelog": *changelogF, "bugreport": *bugreport, "check-update": *checkUpd, "set-passcode": *setPass,
		"theme-install": *themeInst != "", "theme-update": *themeUpd,
	}, flag.NArg()); err != nil {
		return err
	}
	switch { // these never touch the config, so they work even when it is broken
	case *version:
		fmt.Println("webtuidb", versionString())
		return nil
	case *man:
		fmt.Print(manPage)
		return nil
	case *changelogF:
		changelog(os.Stdout)
		return nil
	case *bugreport:
		bugReport(os.Stdout, versionString(), *cfgPath)
		return nil
	case *checkUpd:
		msg, err := checkUpdate(&http.Client{}, latestAPIURL, versionString())
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	case *setPass:
		return setPasscode(os.Stdout)
	case *themeInst != "":
		return themeInstall(os.Stdout, *themeInst)
	case *themeUpd:
		return themeUpdate(os.Stdout)
	case *mcpConfig != "":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		var extra []string
		if flag.Lookup("config") != nil && configGiven(flag.CommandLine) {
			abs, aerr := filepath.Abs(*cfgPath)
			if aerr != nil {
				return aerr
			}
			extra = []string{"--config", abs}
		}
		snippet, file, err := mcpSnippet(*mcpConfig, exe, extra...)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "# put this in", file) // stderr: stdout stays a valid file body
		fmt.Print(snippet)
		return nil
	case *completion != "":
		s, err := completionScript(*completion, flag.CommandLine)
		if err != nil {
			return err
		}
		fmt.Print(s)
		return nil
	}
	if *sshLog != "" {
		f, err := os.OpenFile(*sshLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("--ssh-log: %w", err)
		}
		defer f.Close()
		db.SSHLog = f
	}
	if !*frontend {
		var stray []string
		flag.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "listen", "open", "token", "insecure-listen":
				stray = append(stray, "--"+f.Name)
			}
		})
		if len(stray) > 0 {
			return fmt.Errorf("%s only works together with --frontend", strings.Join(stray, ", "))
		}
	}

	if !filepath.IsAbs(config.ConfigDir()) || !filepath.IsAbs(config.DataDir()) {
		return fmt.Errorf("cannot find your home directory (HOME is unset); set HOME or XDG_CONFIG_HOME/XDG_DATA_HOME to absolute paths")
	}
	if err := config.MigrateLegacy(); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	km, pluginConflicts, err := cfg.KeymapChecked(plugin.Bindings()) // the same map as the help screens (plugin shortcuts included)
	if err != nil {
		return fmt.Errorf("%s: %w", *cfgPath, err)
	}
	if *theme {
		fmt.Print(ui.ThemeTOML(cfg.Theme))
		return nil
	}
	if *keys {
		for _, c := range pluginConflicts {
			fmt.Fprintln(os.Stderr, "webtuidb: plugin shortcut skipped:", c)
		}
		printKeys(cfg.General.Prefix, km)
		return nil
	}
	connPath := config.ConnectionsPath()
	conns, err := config.LoadAllConnections(connPath, cfg.Sources)
	if err != nil {
		return err
	}
	if *listConns {
		for _, c := range conns {
			fmt.Println(c.Name)
		}
		return nil
	}
	data := config.DataDir()
	connNames := make([]string, len(conns))
	for i, c := range conns {
		connNames[i] = c.Name
	}
	(&store.History{Dir: filepath.Join(data, "history")}).MigrateLegacy(connNames)
	for _, c := range conns {
		if c.Unusable != "" {
			fmt.Fprintf(os.Stderr, "webtuidb: connection %q is unusable: %s\n", c.Name, c.Unusable)
		}
	}
	if *mcpStdio {
		srv := mcp.New(cfg, conns)
		srv.Queries = &store.Queries{Root: filepath.Join(data, "queries")}
		srv.Hist = &store.History{Dir: filepath.Join(data, "history")}
		srv.Boards = true
		return srv.ServeStdio(context.Background())
	}
	defer crashlog.Recover(nil) // shortcut: the console log lives in each workspace, so no console lines here; wire store.Console when it is reachable
	state, err := store.LoadState(filepath.Join(data, "state.json"))
	if err != nil {
		return err
	}
	if state.Recovered != "" {
		fmt.Fprintln(os.Stderr, "webtuidb:", state.Recovered)
	}
	d := &ui.Deps{
		Cfg: cfg, CfgPath: *cfgPath, Keys: km, Conns: conns, ConnPath: connPath, State: state,
		Hist:    &store.History{Dir: filepath.Join(data, "history")},
		Queries: &store.Queries{Root: filepath.Join(data, "queries")},
	}
	if *frontend {
		if *token == "" {
			*token = os.Getenv("WEBTUIDB_TOKEN")
		}
		if pw := os.Getenv("WEBTUIDB_PASSWORD"); pw != "" {
			cfg.Web.Password = pw
		}
		if *insecure && *token == "" && cfg.Web.Password == "" && os.Getenv("WEBTUIDB_INSECURE") != "1" {
			return fmt.Errorf("--insecure-listen without authentication: set WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or [web] password / --token), or WEBTUIDB_INSECURE=1 to accept an open server")
		}
		return web.Run(web.Options{
			Cfg: cfg, Conns: conns, ConnPath: connPath, CfgPath: *cfgPath, State: state, Hist: d.Hist, Queries: d.Queries,
			Listen: *listen, Token: *token, Open: *openBrowser, Insecure: *insecure, Version: versionString(),
		})
	}
	app := ui.NewApp(d)

	if arg := flag.Arg(0); arg != "" {
		c, err := pick(d, arg)
		if err != nil {
			return err
		}
		app.StartWith(c)
	}
	// WithoutCatchPanics lets a panic in Update/View reach crashlog.Recover above; Kill restores the terminal first.
	// shortcut: a panic inside a tea.Cmd goroutine is not on this stack and still ends the process without a log.
	prog := tea.NewProgram(app, tea.WithoutCatchPanics())
	defer func() {
		if r := recover(); r != nil {
			prog.Kill()
			panic(r)
		}
	}()
	_, err = prog.Run()
	return err
}

// versionString is Version, or for `go install` builds the module version the toolchain recorded.
func versionString() string {
	if bi, ok := debug.ReadBuildInfo(); ok && Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// mcpSnippet returns the stdio server entry for an MCP client and the file it belongs in.
func mcpSnippet(client, exe string, extra ...string) (snippet, file string, err error) {
	q := fmt.Sprintf("%q", exe)
	args := append([]string{"--mcp"}, extra...)
	jb, _ := json.Marshal(args)
	ja := string(jb)
	switch client {
	case "claude-code":
		return fmt.Sprintf("{\"mcpServers\":{\"webtuidb\":{\"command\":%s,\"args\":%s}}}\n", q, ja), ".mcp.json (project) or ~/.claude.json", nil
	case "cursor":
		return fmt.Sprintf("{\"mcpServers\":{\"webtuidb\":{\"command\":%s,\"args\":%s}}}\n", q, ja), "~/.cursor/mcp.json", nil
	case "vscode":
		return fmt.Sprintf("{\"servers\":{\"webtuidb\":{\"type\":\"stdio\",\"command\":%s,\"args\":%s}}}\n", q, ja), ".vscode/mcp.json", nil
	case "codex": // Codex reads TOML, not JSON
		return fmt.Sprintf("[mcp_servers.webtuidb]\ncommand = %s\nargs = %s\n", q, ja), "~/.codex/config.toml", nil
	}
	return "", "", fmt.Errorf("unknown MCP client %q (claude-code | codex | cursor | vscode)", client)
}

// flagChoices and flagFiles say what a flag's value completes to; every other value-taking flag completes to nothing.
var (
	flagChoices = map[string]string{"mcp-config": "claude-code codex cursor vscode", "completion": "bash zsh fish"}
	flagFiles   = map[string]bool{"config": true, "ssh-log": true}
)

// completionScript completes the flags of fs, and connection names via `webtuidb -list-connections`.
// shortcut: bash inserts a connection name with a space unquoted, quote it by hand.
func completionScript(shell string, fs *flag.FlagSet) (string, error) {
	var b strings.Builder
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	isBool := func(f *flag.Flag) bool {
		bf, ok := f.Value.(interface{ IsBoolFlag() bool })
		return ok && bf.IsBoolFlag()
	}
	switch shell {
	case "bash":
		fmt.Fprintf(&b, "_webtuidb() {\n  local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}\n  case ${prev#-} in\n")
		for _, n := range names {
			switch {
			case flagChoices[n] != "":
				fmt.Fprintf(&b, "    %[1]s|-%[1]s) COMPREPLY=($(compgen -W %q -- \"$cur\")); return;;\n", n, flagChoices[n])
			case flagFiles[n]:
				fmt.Fprintf(&b, "    %[1]s|-%[1]s) COMPREPLY=($(compgen -f -- \"$cur\")); return;;\n", n)
			}
		}
		var dashed []string
		for _, n := range names {
			dashed = append(dashed, "--"+n)
		}
		fmt.Fprintf(&b, "  esac\n  if [[ $cur == -* ]]; then COMPREPLY=($(compgen -W %q -- \"$cur\"))\n  else local n; while IFS= read -r n; do [[ $n == \"$cur\"* ]] && COMPREPLY+=(\"$n\"); done < <(webtuidb -list-connections 2>/dev/null); fi\n}\ncomplete -F _webtuidb webtuidb\n", strings.Join(dashed, " "))
	case "zsh":
		esc := strings.NewReplacer("'", `'\''`, "[", `\[`, "]", `\]`, ":", `\:`)
		b.WriteString("#compdef webtuidb\n_webtuidb() {\n  local -a conns\n  _arguments \\\n")
		fs.VisitAll(func(f *flag.Flag) {
			val := ""
			switch {
			case flagChoices[f.Name] != "":
				val = ":value:(" + flagChoices[f.Name] + ")"
			case flagFiles[f.Name]:
				val = ":file:_files"
			case !isBool(f):
				val = ":value:"
			}
			fmt.Fprintf(&b, "    '--%s[%s]%s' \\\n", f.Name, esc.Replace(f.Usage), val)
		})
		b.WriteString("    '1:connection:->conn'\n  if [[ $state == conn ]]; then\n    conns=(\"${(@f)$(webtuidb -list-connections 2>/dev/null)}\")\n    compadd -a conns\n  fi\n}\n_webtuidb \"$@\"\n")
	case "fish":
		esc := strings.NewReplacer("'", `\'`)
		b.WriteString("complete -c webtuidb -f\n")
		fs.VisitAll(func(f *flag.Flag) {
			opt := ""
			switch {
			case flagChoices[f.Name] != "":
				opt = fmt.Sprintf(" -x -a '%s'", flagChoices[f.Name])
			case flagFiles[f.Name]:
				opt = " -r -F"
			case !isBool(f):
				opt = " -x"
			}
			fmt.Fprintf(&b, "complete -c webtuidb -l %s%s -d '%s'\n", f.Name, opt, esc.Replace(f.Usage))
		})
		b.WriteString("complete -c webtuidb -n __fish_is_first_arg -a '(webtuidb -list-connections 2>/dev/null)'\n")
	default:
		return "", fmt.Errorf("unknown shell %q (bash | zsh | fish)", shell)
	}
	return b.String(), nil
}

func printKeys(prefix string, km *keymap.Map) { fmt.Print(keymap.Listing(prefix, km)) }

// pick resolves the CLI argument: a saved connection name, or a URL (kept in memory, not saved).
func pick(d *ui.Deps, arg string) (config.Connection, error) {
	for _, c := range d.Conns {
		if c.Name == arg {
			return c, nil
		}
	}
	if strings.Contains(arg, "://") {
		c, err := config.ParseURL(arg)
		if err != nil {
			return c, err
		}
		for _, o := range d.Conns { // same name as a saved one: its settings, with every part the URL gave on top
			if c.Name != "" && o.Name == c.Name {
				return overlayURL(o, c), nil // not written back: the saved entry stays as it is
			}
		}
		d.Conns = append(d.Conns, c)
		return c, nil
	}
	return config.Connection{}, fmt.Errorf("no saved connection named %q (and it is not a URL)", arg)
}

// configGiven reports whether --config was passed explicitly.
func configGiven(fs *flag.FlagSet) bool {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == "config" })
	return given
}

// checkModes allows at most one mode flag, and positional arguments only when none is set (the TUI takes a connection or URL).
func checkModes(modes map[string]bool, nargs int) error {
	var set []string
	for name, on := range modes {
		if on {
			set = append(set, "--"+name)
		}
	}
	slices.Sort(set)
	if len(set) > 1 {
		return fmt.Errorf("%s cannot be combined; use only one of --frontend, --mcp, --keys, --theme, --man, --completion, --mcp-config, --list-connections, --version", strings.Join(set, " and "))
	}
	if len(set) == 1 && nargs > 0 {
		return fmt.Errorf("%s takes no connection or URL argument", set[0])
	}
	return nil
}

// overlayURL returns the saved connection with every component the URL supplied laid over it.
func overlayURL(saved, u config.Connection) config.Connection {
	m := saved
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&m.Driver, u.Driver)
	set(&m.Host, u.Host)
	set(&m.User, u.User)
	if u.Password != "" {
		m.Password, m.PasswordCmd = u.Password, ""
	}
	set(&m.Database, u.Database)
	set(&m.SSLMode, u.SSLMode)
	set(&m.Tag, u.Tag)
	set(&m.DriverVersion, u.DriverVersion)
	if u.Port != 0 {
		m.Port = u.Port
	}
	if u.LazyLoad {
		m.LazyLoad = true
	}
	if u.SSH != nil {
		m.SSH = u.SSH
	}
	if u.Open != nil {
		m.Open = u.Open
	}
	if len(u.Options) > 0 {
		m.Options = map[string]string{}
		for k, v := range saved.Options {
			m.Options[k] = v
		}
		for k, v := range u.Options {
			m.Options[k] = v
		}
	}
	return m
}
