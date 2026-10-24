# tuidb

A terminal TablePlus: connect to **PostgreSQL, MySQL, ClickHouse, Redis and MongoDB**, browse and edit
data with pending changes, run queries, and drive everything with tmux-style key bindings:
**prefix, then a key**. The default prefix is `Ctrl+d`; every prefixed action can be rebound in
`~/.config/tuidb/config.toml`. The plain keys inside a pane (`j`/`k`, `/`, `s`, `p` …) are fixed.

```
make build          # go build -o tuidb .
./tuidb             # connection list
./tuidb pg-local    # connect to a saved connection
./tuidb postgresql://user:pw@host/db?name=Prod&env=production
./tuidb -keys       # print every binding as it applies with your config
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
# ~/.config/tuidb/config.toml
[general]
prefix = "C-a"                 # tmux-style key names: C-x, M-x, S-Tab, F5, Enter, Space, ]

[keys]
commit  = "x"                  # taking a key replaces whatever was bound to it (like tmux bind)
run_all = ["M-Enter", "-n F5"] # a list binds several keys; "-n " means no prefix needed
```

Unknown actions, unparsable keys, two of your bindings on the same key, and binding `esc`
(it cancels the prefix) are reported at startup with the offending line.

In a grid, `Enter` opens the cell in a popover editor (JSON is pretty-printed; `Ctrl+s` saves, `Esc` cancels)
and `e` edits inline. Also handy: `/` find (then `n` / `N`), `s` sort the column, `#` exact row count, `L` limit and offset,
paste rows (`v`) and cells (`V`), and in the results pane `p` pins a result so the next run keeps it. A query result
is editable when it is a plain `SELECT ... FROM table` that includes the primary key.

## Connections

`~/.config/tuidb/connections.toml` (created with mode 0600, edited from the connection list with
`n`/`e`/`c`/`d`, or import a URL with `i`). **Passwords are stored in plain text**; to keep them out of
the file use `password_cmd`, whose stdout becomes the password. It is run directly, not through a shell: quotes group
arguments, but pipes and variables do not work:

```toml
[[connection]]
name = "Acme prod"
driver = "postgres"            # postgres | mysql | clickhouse | redis | mongodb
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

## Web UI

```sh
make web build        # builds web/ into the binary (needs Node 20+; plain `make build` works without it, API only)
tuidb --frontend --open
```

`--frontend` is opt-in: without it tuidb starts the terminal UI and never opens a port. With it, tuidb serves the same engine over a local HTTP API plus a browser app (TanStack Router, Query, Table and Virtual, CodeMirror). It shares `connections.toml`, query history, saved queries, pinned and recent tables and per-table filters/sort with the terminal UI, and enforces each connection's Safe Mode on the server.

| Flag (with `--frontend`) | Meaning |
|---|---|
| `--listen addr` | default `127.0.0.1:7788`; non-loopback addresses are refused unless `--insecure-listen` |
| `--open` | open the browser at the printed URL |
| `--token t` | API token; default is random, printed once as `http://.../#token=...` |

The token travels in the URL fragment (never sent to the server or logged) and is kept in `sessionStorage`. For frontend development run `tuidb --frontend --token dev` and `cd web && npm run dev`, which proxies `/api` to port 7788.

## Data and state

Query history, saved queries and UI state live under `~/.local/share/tuidb/`
(`history/<connection>.jsonl`, `queries/*.sql`, `state.json`).

## Development

```
make up        # five test databases in docker compose on 127.0.0.1 (ports 55432 53306 59000 56379 57017), user/password tuidb, seeded
make test      # unit tests
make smoke     # + driver integration tests for all five engines and end-to-end UI tests on PostgreSQL and MySQL
make down
```

The plan and what is built so far: [PLAN.md](PLAN.md).
