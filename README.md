# webtuidb

A terminal TablePlus: connect to **PostgreSQL, MySQL, MariaDB, ClickHouse, Cassandra, DynamoDB, Redis and MongoDB**
(plus Redshift, Greenplum and Vertica on the PostgreSQL driver, and SingleStore on MySQL's), browse and edit
data with pending changes, run queries, and drive everything with tmux-style key bindings:
**prefix, then a key**. The default prefix is `Ctrl+d`; every prefixed action can be rebound in
`~/.config/webtuidb/config.toml`. The plain keys inside a pane (`j`/`k`, `/`, `s`, `p` …) are fixed.

```
./webtuidb             # connection list
./webtuidb pg-local    # connect to a saved connection
./webtuidb postgresql://user:pw@host/db?name=Prod&env=production
./webtuidb -keys       # print every binding as it applies with your config
./webtuidb -version
```

## Install

```sh
# a release binary (linux, macOS, Windows; amd64, arm64): download it from the Releases page, or
brew install --HEAD outofmemry/tap/webtuidb

# Docker: the web UI on port 7788 (mount your config so it sees your connections).
# The container listens on 0.0.0.0 over plain HTTP, so it refuses to start unless WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN is set
# (WEBTUIDB_INSECURE=1 accepts an open server explicitly). --user lets the container read and chmod your bind-mounted files;
# publish on 127.0.0.1 and put TLS in front of anything beyond that.
docker build -t webtuidb . && docker run -p 127.0.0.1:7788:7788 -e WEBTUIDB_PASSWORD='choose-one' --user $(id -u) -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb webtuidb

# from source (needs Go, and Node 22.18+ for the web app)
make web build      # web app + go build -o webtuidb .
go install .        # from a clone (webtuidb is not a published module path); terminal UI and API only: the web app is built by `make web`, not by go install
```

`make release` builds all the archives into `dist/` with goreleaser (`--snapshot`, nothing is published).

### Completions and man page

```sh
source <(webtuidb -completion bash)                       # or zsh; fish: webtuidb -completion fish | source
webtuidb -man | sudo tee /usr/local/share/man/man1/webtuidb.1   # man webtuidb
```

Completions cover the flags and your saved connection names.

### MCP server

`webtuidb --mcp` serves the MCP tools over stdio. Print the entry for your client, with the path of this binary filled in:

```sh
webtuidb -mcp-config claude-code   # or codex | cursor | vscode; the file it goes in is printed on stderr
# {"mcpServers":{"webtuidb":{"command":"/usr/local/bin/webtuidb","args":["--mcp"]}}}
```

## Keys

Press `Ctrl+d ?` for the list for the pane you are in; in that list `/` fuzzy-searches by name,
description or key (`fltr`, `commit`, `M-Enter`), and `Enter` runs the selected command. After the prefix a panel lists the keys that
are valid right there; `Esc` cancels. `Ctrl+d Ctrl+d` sends a literal `Ctrl+d` to the focused widget.
Typing, arrows, `j/k` and `Enter` inside a widget are never prefixed. The mouse wheel scrolls any pane (three rows per tick); shift-drag selects terminal text.

Move between panes with `Ctrl+h/j/k/l` (no prefix). Common prefixed ones (defaults): `p` open anything, `e` new SQL editor, `k` open database, `K` open connection,
`s` commit, `P` preview the SQL, `Backspace` discard, `z`/`Z` undo/redo, `f` filters, `x` quick filter,
`o` follow a foreign key, `alt+f` choose columns, `(` `)` `alt+i` `alt+d` data / structure / index / DDL,
`J` `T` Json / Tree views (MongoDB, Redis), `S` safe mode, `alt+enter` run all statements, `.` cancel.

### Rebinding

```toml
# ~/.config/webtuidb/config.toml
[general]
prefix = "C-a"                 # tmux-style key names: C-x, M-x, S-Tab, F5, Enter, Space, ]

[keys]
commit  = "x"                  # taking a key replaces whatever was bound to it (like tmux bind)
run_all = ["M-Enter", "-n F5"] # a list binds several keys; "-n " means no prefix needed
```

Unknown actions, unparsable keys, two of your bindings on the same key, and binding `esc`
(it cancels the prefix) are reported at startup with the offending line.

In a grid, `Enter` opens the cell in a popover editor (JSON is pretty-printed; `Ctrl+s` saves, `Esc` cancels)
and `e` edits inline. Also handy: `/` find (then `n` / `N`; after the prefix the same actions are `alt+/` find, `alt+g` find next, `alt+G` find previous, so the prefix `/` and `n` keep their global meaning), `s` sort the column, `#` exact row count, `L` limit and offset,
paste rows (`v`) and cells (`V`), and in the results pane `p` pins a result so the next run keeps it. A query result
is editable when it is a plain `SELECT ... FROM table` that includes the primary key.

In the filter bar `alt+n` adds a filter as a copy of this one, `alt+c` clears all filters and `alt+e` exports the
filtered rows (actions `filter_add_copy`, `filter_clear_all`, `filter_export`).

## Connections

`~/.config/webtuidb/connections.toml` (created with mode 0600, edited from the connection list with
`n`/`e`/`c`/`d`, or import a URL with `i`). **Passwords are stored in plain text**; to keep them out of
the file use `password_cmd`, whose stdout becomes the password. It is run directly, not through a shell: quotes group
arguments, but pipes and variables do not work:

```toml
[[connection]]
name = "Acme prod"
driver = "postgres"            # postgres | mysql | mariadb | redshift | greenplum | vertica | singlestore | clickhouse | cassandra | dynamodb | redis | mongodb
host = "db.internal"
user = "app"
password_cmd = "op read op://Private/acme-prod/password"
database = "acme"
tag = "production"             # local testing development staging production, or your own
safe_mode = "alert2"           # silent | alert1 | alert2 | safe1 | safe2
group = "Acme"
ssl_mode = "verify-full"
[connection.ssh]
host = "bastion.example.com"
user = "me"
key = "~/.ssh/id_ed25519"      # empty: ssh-agent
```

Safe mode works as in TablePlus: Alert modes ask for confirmation (showing the exact statements),
Safe modes ask for the database password. Mode 2 lets `SELECT`/`EXPLAIN`/`SHOW` through.

### Local servers (DBngin, Docker, Homebrew)

Import a local server with `i` in the connection list (or paste the URL as the argument: `webtuidb 'postgresql://...'`). The ports below are the defaults; use the port the tool shows for your instance.

| Server | Import URL |
|---|---|
| DBngin PostgreSQL | `postgresql://postgres@127.0.0.1:5432/postgres` |
| DBngin MySQL / MariaDB | `mysql://root@127.0.0.1:3306/` |
| DBngin Redis | `redis://127.0.0.1:6379/0` |
| Docker `postgres` (`-e POSTGRES_PASSWORD=pw -p 5432:5432`) | `postgresql://postgres:pw@127.0.0.1:5432/postgres` |
| Docker `mysql` (`-e MYSQL_ROOT_PASSWORD=pw -p 3306:3306`) | `mysql://root:pw@127.0.0.1:3306/` |
| Docker `mongo` | `mongodb://127.0.0.1:27017/` |
| Homebrew `postgresql@16` (`brew services start`) | `postgresql://$USER@127.0.0.1:5432/postgres` |
| Homebrew `mysql` / `mariadb` | `mysql://root@127.0.0.1:3306/` |
| Homebrew `redis` | `redis://127.0.0.1:6379/0` |

Add `?name=Local&env=local` to name the connection and tag it. A Unix socket is not needed: `127.0.0.1` works for all of them.

### Settings

`webtuidb` reads `~/.config/webtuidb/config.toml` over its defaults, and both UIs have a settings editor (it rewrites the file; comments in it are not kept). New keys:

| Key | Default | Meaning |
|---|---|---|
| `[general] hints` | `[]` | actions shown in the terminal status line, in order (empty = built-in set) |
| `[general] connection_sort` | `"name"` | `name`, `tag`, `driver` or `recent` (last connected first) |
| `[web] auto_hide_scrollbars` | `true` | scrollbars only while scrolling |
| `[web] toolbar` | `[]` | web toolbar actions, in order (empty = built-in set) |
| `[security] passcode` | unset | bcrypt hash; make it with `webtuidb -set-passcode` |
| `[security] lock_after_mins` | `0` | lock after this many idle minutes (0 = never) |
| `[table] decode_spatial` | `true` | geometry cells as WKT instead of hex |
| `[table] image_preview` | `true` | recognise image bytes and preview them in quick look |

Plugin shortcuts are keys named `plugin:<name>` and can be rebound under `[keys]`. Themes installed with `webtuidb -theme-install <git url>[@ref]` (a repo with a top-level `theme.toml`, cloned into `~/.config/webtuidb/themes/`) can be used as `[theme] name`; `-theme-update` pulls them.

### Help menu equivalents

Things the desktop help menu would do are flags here: `webtuidb -changelog` opens the release notes (and prints the URL), `webtuidb -bugreport` prints version, OS, `$TERM`, the drivers your connections use and your config with secrets masked (paste it into an issue), `webtuidb -check-update` asks GitHub once whether a newer release exists. Nothing checks for updates on its own. A crash writes the panic, stack and last console lines to `~/.local/share/webtuidb/crash.log`; nothing is sent anywhere.

## Web UI

```sh
make web build        # builds web/ into the binary (needs Node 22.18+; plain `make build` works without it, API only)
webtuidb --frontend --open
```

`--frontend` is opt-in: without it webtuidb starts the terminal UI and never opens a port. With it, webtuidb serves the same engine over a local HTTP API plus a browser app (TanStack Router, Query, Table and Virtual, CodeMirror). It shares `connections.toml`, query history, saved queries, pinned and recent tables and per-table filters/sort with the terminal UI, and enforces each connection's Safe Mode on the server.

| Flag (with `--frontend`) | Meaning |
|---|---|
| `--listen addr` | default `127.0.0.1:7788`; non-loopback addresses are refused unless `--insecure-listen` |
| `--open` | open the browser at the printed URL |
| `--token t` | API token; default is random, printed once as `http://.../#token=...` |

The token travels in the URL fragment (never sent to the server or logged) and is kept in `sessionStorage`. For frontend development run `webtuidb --frontend --token dev` and `cd web && npm run dev`, which proxies `/api` to port 7788.

## Data and state

Query history, saved queries and UI state live under `~/.local/share/webtuidb/`
(`history/<connection>.jsonl`, `queries/*.sql`, `state.json`).

The rename from `tuidb` keeps existing data: the first run moves `~/.config/tuidb` and
`~/.local/share/tuidb` to the new names, so connections, history and state carry over. If a `webtuidb`
directory already exists it wins, and the old one is left untouched.

## Development

```
make up        # test databases in docker compose on 127.0.0.1 (see docker-compose.yml for the ports), user/password webtuidb, seeded
make test      # unit tests
make smoke     # + driver integration tests for every engine and end-to-end UI tests on PostgreSQL and MySQL
make down
make parity    # both UIs expose the same menus, actions and config keys (also runs in CI)
```

The plan and what is built so far: [docs/PLAN.md](docs/PLAN.md).
