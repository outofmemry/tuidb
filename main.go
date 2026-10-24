// tuidb is a terminal TablePlus: connect to PostgreSQL, MySQL, MariaDB, CockroachDB, SQLite, DuckDB, ClickHouse, Cassandra, DynamoDB, Cloudflare D1, Redis and MongoDB,
// browse and edit data, and run queries, with tmux-style prefix key bindings.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tuidb/internal/config"
	"tuidb/internal/keymap"
	"tuidb/internal/store"
	"tuidb/internal/ui"
	"tuidb/internal/web"

	_ "tuidb/internal/db/cassandra"
	_ "tuidb/internal/db/clickhouse"
	_ "tuidb/internal/db/cockroach"
	_ "tuidb/internal/db/d1"
	_ "tuidb/internal/db/duckdb"
	_ "tuidb/internal/db/dynamodb"
	_ "tuidb/internal/db/mariadb"
	_ "tuidb/internal/db/mongo"
	_ "tuidb/internal/db/mysql"
	_ "tuidb/internal/db/postgres"
	_ "tuidb/internal/db/redis"
	_ "tuidb/internal/db/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tuidb:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", filepath.Join(config.ConfigDir(), "config.toml"), "path to config.toml")
	keys := flag.Bool("keys", false, "print the key bindings (after config overrides) and exit")
	frontend := flag.Bool("frontend", false, "serve the web UI instead of starting the terminal UI")
	listen := flag.String("listen", "127.0.0.1:7788", "with --frontend: address to listen on")
	openBrowser := flag.Bool("open", false, "with --frontend: open the browser")
	token := flag.String("token", "", "with --frontend: API token (default: random)")
	insecure := flag.Bool("insecure-listen", false, "with --frontend: allow a non-loopback --listen address (credentials go over the network)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tuidb [flags] [connection-name | URL]\n       tuidb --frontend [--listen addr] [--open]\n\n  URL: postgresql:// mysql:// mariadb:// cockroachdb:// sqlite:// duckdb:// clickhouse:// cassandra:// dynamodb:// d1:// redis:// mongodb://\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
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
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	km, err := cfg.Keymap()
	if err != nil {
		return fmt.Errorf("%s: %w", *cfgPath, err)
	}
	if *keys {
		printKeys(cfg.General.Prefix, km)
		return nil
	}
	connPath := config.ConnectionsPath()
	conns, err := config.LoadConnections(connPath)
	if err != nil {
		return err
	}
	data := config.DataDir()
	state, err := store.LoadState(filepath.Join(data, "state.json"))
	if err != nil {
		return err
	}
	if state.Recovered != "" {
		fmt.Fprintln(os.Stderr, "tuidb:", state.Recovered)
	}
	d := &ui.Deps{
		Cfg: cfg, Keys: km, Conns: conns, ConnPath: connPath, State: state,
		Hist:    &store.History{Dir: filepath.Join(data, "history")},
		Queries: &store.Queries{Root: filepath.Join(data, "queries")},
	}
	if *frontend {
		if flag.NArg() > 0 {
			return fmt.Errorf("--frontend takes no connection argument; pick the connection in the browser")
		}
		return web.Run(web.Options{
			Cfg: cfg, Conns: conns, ConnPath: connPath, State: state, Hist: d.Hist, Queries: d.Queries,
			Listen: *listen, Token: *token, Open: *openBrowser, Insecure: *insecure,
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
	_, err = tea.NewProgram(app).Run()
	return err
}

func printKeys(prefix string, km *keymap.Map) {
	fmt.Printf("prefix: %s   (every action below is: prefix, then the key; \"-n\" bindings need no prefix)\n\n", prefix)
	for _, ctx := range []string{keymap.Global, keymap.Sidebar, keymap.Grid, keymap.FilterBar, keymap.Structure, keymap.Editor, keymap.Results} {
		var rows []keymap.Binding
		for _, b := range km.All() {
			if b.Context == ctx {
				rows = append(rows, b)
			}
		}
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Action < rows[j].Action })
		fmt.Printf("[%s]\n", ctx)
		for _, b := range rows {
			k := keymap.Display(b.Key)
			if b.NoPrefix {
				k = "-n " + k
			}
			fmt.Printf("  %-26s %-10s %s\n", b.Action, k, b.Desc)
		}
		fmt.Println()
	}
}

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
		for _, o := range d.Conns { // same name as a saved one: use the saved settings
			if o.Name == c.Name {
				return o, nil
			}
		}
		d.Conns = append(d.Conns, c)
		return c, nil
	}
	return config.Connection{}, fmt.Errorf("no saved connection named %q (and it is not a URL)", arg)
}
