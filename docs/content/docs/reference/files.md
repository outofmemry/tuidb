---
title: Files, environment, exit codes
description: Every file and folder webtuidb reads or writes, the environment variables it honours, and its exit codes.
weight: 50
---

A short index of everything webtuidb keeps on disk, the environment variables it reads and what its exit status means. Both UIs use the same files: the terminal UI and `webtuidb --frontend` can run at the same time against one configuration, and every shared file is written under a lock. For what is inside each file, see [Files and folders](../../configuration/files-and-folders/).

## Folders

webtuidb uses two folders, following the XDG base directory rules:

| Folder | Default | Override |
|---|---|---|
| Config folder | `~/.config/webtuidb` | `$XDG_CONFIG_HOME/webtuidb` |
| Data folder | `~/.local/share/webtuidb` | `$XDG_DATA_HOME/webtuidb` |

An `XDG_*` variable is used only when it holds an absolute path; a relative one is ignored, as the XDG specification says. With no home directory and no absolute `XDG_*` paths, webtuidb stops with `cannot find your home directory (HOME is unset); set HOME or XDG_CONFIG_HOME/XDG_DATA_HOME to absolute paths`.

The first run after the rename from `tuidb` moves `~/.config/tuidb` and `~/.local/share/tuidb` to the new names. When a `webtuidb` folder already exists it wins and the old one is left alone; a move that fails stops startup instead of starting empty.

## Every file

{{< filetree/container >}}
  {{< filetree/folder name="~/.config/webtuidb" >}}
    {{< filetree/file name="config.toml" >}}
    {{< filetree/file name="connections.toml" >}}
    {{< filetree/file name="connections.toml.lock" >}}
    {{< filetree/folder name="themes" state="closed" >}}
      {{< filetree/file name="<name>/theme.toml or <name>.toml" >}}
    {{< /filetree/folder >}}
    {{< filetree/folder name="plugins" state="closed" >}}
      {{< filetree/file name="*.toml, *.js" >}}
    {{< /filetree/folder >}}
  {{< /filetree/folder >}}
  {{< filetree/folder name="~/.local/share/webtuidb" >}}
    {{< filetree/file name="state.json" >}}
    {{< filetree/file name="state.json.lock" >}}
    {{< filetree/folder name="history" state="closed" >}}
      {{< filetree/file name="<connection>.jsonl (+ .lock)" >}}
    {{< /filetree/folder >}}
    {{< filetree/folder name="queries" state="closed" >}}
      {{< filetree/file name="<folder>/<name>.sql" >}}
    {{< /filetree/folder >}}
    {{< filetree/folder name="chats" state="closed" >}}
      {{< filetree/file name="<connection>/<id>.json" >}}
    {{< /filetree/folder >}}
    {{< filetree/folder name="boards" state="closed" >}}
      {{< filetree/file name="<board>.toml" >}}
    {{< /filetree/folder >}}
    {{< filetree/folder name="backups" state="closed" >}}
      {{< filetree/file name="(web backups and restores)" >}}
    {{< /filetree/folder >}}
    {{< filetree/file name="crash.log, crash.log.1" >}}
    {{< filetree/file name="ssh.log" >}}
  {{< /filetree/folder >}}
{{< /filetree/container >}}

| Path | What it is | Written by |
|---|---|---|
| `config.toml` | settings, key bindings, theme, AI, MCP, web and security options. `--config path` reads another file. A missing file means the defaults. | you; the Settings dialog in either UI (comments in the file are not kept) |
| `connections.toml` | saved connections, passwords included. Kept at mode 600: a group- or world-readable file is fixed at startup, see [Troubleshooting](../troubleshooting/#connectionstoml-permissions). | both UIs, through a temporary file and a rename |
| `*.lock` | an empty lock file next to a shared file (`connections.toml.lock`, `state.json.lock`, `history/<connection>.jsonl.lock`, beside saved queries): one process writes at a time. Safe to delete while webtuidb is not running. | both UIs |
| `[[source]]` folders | extra connection files (`*.toml` in `connections.toml` format) named in `config.toml`. Read only: webtuidb never changes or chmods them. | you |
| `themes/` | installed themes; `webtuidb -theme-install <git url>[@ref]` clones into it, `-theme-update` pulls. | `-theme-install`, `-theme-update` |
| `plugins/` | plugins: TOML command plugins and JavaScript plugins. | you |
| `state.json` | UI state: pinned and recent objects, per-table filters, sort and column widths, open tabs and workspaces, folds, row limits, sidebar groups and colours, backup options, recent files, connection sort. A file that is not valid JSON is moved to `state.json.corrupt` and webtuidb starts with empty state, saying so. | both UIs |
| `history/<connection>.jsonl` | query history, one JSON line per statement. A connection name with unusual characters is percent-encoded in the file name. | both UIs |
| `queries/` | saved queries, one `.sql` file each, folders allowed. `[queries] shared_dirs` adds read-only folders beside it. | both UIs |
| `chats/<connection>/` | AI assistant conversations, one JSON file per chat. | both UIs |
| `boards/` | metrics boards, one TOML file per board. The web keeps a board's panel arrangement in the browser instead. | both UIs |
| `backups/` | backups and restores started from the web: the web only reads and writes files in this folder. The terminal writes where you point it. | web UI |
| `crash.log` | panics: time, stack and the last console lines. Rolls over to `crash.log.1` past 2 MB. Nothing is sent anywhere. | both UIs |
| `ssh.log` | SSH tunnel trace switched on from Open anything ("SSH log"). `--ssh-log FILE` writes to a file you name instead. | terminal UI, `--ssh-log` |

Other files webtuidb writes:

- **Exports and terminal backups** go to the path in the dialog's File field. A relative path is relative to the folder you started webtuidb in. The default name comes from `[export] filename` (`{table}-{date}`). Web exports download through the browser.
- **Backups in progress** are written to `<file>.part` and renamed when the tool finishes, so a half-written dump never has the final name.
- **Temporary files** in the system temp folder, removed when done: `webtuidb-import-*` (web imports), `webtuidb-export-*` (web exports and board exports), `webtuidb-mongo-*.yaml` (the 600 config file that passes a MongoDB password to `mongodump` / `mongorestore`).
- **SQL files** you open or save with `open_file`, `save_sql_as` and `open_recent`; the recent list lives in `state.json`.

Some things the web keeps in the browser only (`localStorage` or `sessionStorage`), not on disk: the access token, the list of last workspaces, sidebar item colours, ER diagram positions and colours, and the metrics board layout. A different browser or port starts without them. See [Known limits](../known-limits/#web-ui).

## Environment variables

| Variable | Effect |
|---|---|
| `XDG_CONFIG_HOME` | base of the config folder (absolute paths only) |
| `XDG_DATA_HOME` | base of the data folder (absolute paths only) |
| `HOME` | used for `~` and the default folders |
| `WEBTUIDB_TOKEN` | with `--frontend`: the API token when `--token` is not given |
| `WEBTUIDB_PASSWORD` | with `--frontend`: a login password, overriding `[web] password` |
| `WEBTUIDB_INSECURE` | `1` lets `--insecure-listen` start without a token or password (an open server) |
| `WEBTUIDB_DEV_ORIGIN` | comma-separated origins, such as `http://localhost:5173`, allowed to call the API besides the server's own (a front end served from another port) |
| `WEBTUIDB_COLORS` | `16` forces the 16-colour palette in the terminal; any other value (`truecolor`, `256`) forbids it |
| `COLORTERM`, `TERM` | colour detection: without `COLORTERM`, a `TERM` that names no rich terminal (`256`, `direct`, `truecolor`, `kitty`, `ghostty`, `wezterm`, `alacritty`) gets the 16-colour palette |
| `TERM_PROGRAM` | `Apple_Terminal` means OSC 52 copying cannot reach the clipboard |
| `WEBTUIDB_OSC52` | `1` copies with OSC 52 even outside SSH |
| `SSH_TTY`, `SSH_CONNECTION` | either one makes copying use OSC 52, so text reaches the clipboard of the machine you sit at |
| `WAYLAND_DISPLAY`, `DISPLAY` | which Linux clipboard tool is tried: `wl-copy`, or `xclip` / `xsel` |
| `SSH_AUTH_SOCK` | the ssh-agent offered for SSH tunnels |
| `USER` | the default user of an SSH ProxyJump hop |
| `VISUAL`, `EDITOR` | the editor the terminal Settings dialog opens with `e` (default `vi`) |
| `PATH` | where `pg_dump`, `mysqldump`, `mongodump`, `redis-cli` and the other backup tools are found, unless the connection names a tool path |
| `$NAME` in paths | `[theme] file`, `[[source]] path` and `[queries] shared_dirs` expand environment variables and a leading `~/` |

Backup and restore tools run with your environment plus the password in the variable each tool reads, so it never shows in the process list: `PGPASSWORD` and `PGDATABASE` for the PostgreSQL tools, `MYSQL_PWD` for the MySQL and MariaDB tools, `REDISCLI_AUTH` for `redis-cli`. MongoDB gets a temporary config file instead (see above).

For web front-end development, `WEBTUIDB_API` sets where the Vite dev server proxies `/api` (default `127.0.0.1:7788`); it is read by `web/`, not by the binary. Command-line flags are listed in [CLI](../../automation/cli/).

## Exit codes

| Code | When |
|---|---|
| `0` | normal exit: you quit, or a print-and-exit flag (`-keys`, `-version`, `-bugreport`, …) finished |
| `1` | an error stopped webtuidb; the message is printed to standard error as `webtuidb: <message>`. Examples: a rejected `config.toml`, an unknown connection name, two mode flags combined, `--listen` without `--frontend`, a web server that cannot listen. |
| `2` | a crash (Go panic): the details are in `crash.log`, and the terminal is restored first. The Go runtime also prints the panic and its stack to standard error, which sets this status. A panic inside one web request or job does not stop the server: that request fails with `internal error: … (see …/crash.log)`. |

Warnings that do not stop startup also go to standard error with the `webtuidb:` prefix, for example a `[keys]` entry that left another action unbound, a connection marked unusable, or a recovered `state.json`.
