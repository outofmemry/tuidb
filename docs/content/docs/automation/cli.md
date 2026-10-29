---
title: Command line
description: Every webtuidb flag with an example - the connection or URL argument, --frontend, --mcp and -mcp-config, -keys, -theme, completion scripts, -bugreport, -check-update, -set-passcode, theme installs - plus the one-mode rule, environment variables and exit codes.
weight: 40
---

`webtuidb` with no argument opens the terminal UI on the connection list. Everything else is a flag or a
connection to open. Flags take one or two dashes (`-keys` and `--keys` are the same) and a value after a space or
`=` (`--listen 127.0.0.1:8080`, `--listen=127.0.0.1:8080`).

```text
usage: webtuidb [flags] [connection-name | URL]
       webtuidb --frontend [--listen addr] [--open]

  URL: postgresql:// mysql:// mariadb:// redshift:// greenplum:// vertica:// singlestore:// clickhouse:// cassandra:// dynamodb:// redis:// mongodb://
```

`webtuidb -help` prints this with the flag list and exits.

## Opening a connection

```console
$ webtuidb "Shop (Postgres)"                                   # a saved connection, by name
$ webtuidb postgresql://webtuidb:secret@127.0.0.1:55432/shop   # a URL, kept in memory only
```

The argument is first looked up as a saved connection name (exact, case-sensitive). Otherwise, when it contains
`://`, it is parsed as a [connection URL](../../connections/connection-urls/): the connection opens without being
saved. A URL whose name (the `name=` option) matches a saved connection opens that connection with every part
the URL gives laid over it (host, port, user, password, database, SSL mode, tag, options, SSH…); the saved entry is
not changed. Anything else fails: `no saved connection named "x" (and it is not a URL)`.

## Modes

These flags each do one thing and exit (or, for `--frontend` and `--mcp`, serve until stopped). At most one may be
given, and none of them takes a connection argument:

```console
$ webtuidb --keys --version
webtuidb: --keys and --version cannot be combined; use only one of --frontend, --mcp, --keys, --theme, --man, --completion, --mcp-config, --list-connections, --version, --changelog, --bugreport, --check-update, --set-passcode, --theme-install, --theme-update
$ webtuidb --version "Shop (MySQL)"
webtuidb: --version takes no connection or URL argument
```

| Flag | Does | Example |
| --- | --- | --- |
| `-frontend` | serve the [web UI](../../web-ui/serving/) instead of the terminal UI | `webtuidb --frontend --open` |
| `-mcp` | serve the [MCP tools](../mcp-server/) over stdio until the input ends | `webtuidb --mcp` (run by the MCP client) |
| `-mcp-config client` | print the MCP server entry for `claude-code`, `codex`, `cursor` or `vscode`; the file it belongs in goes to standard error | `webtuidb -mcp-config vscode > .vscode/mcp.json` |
| `-keys` | print the key bindings after `[general] prefix`, `[keys]` and plugin shortcuts | `webtuidb -keys \| less` |
| `-theme` | print the resolved theme as `[theme.colors]` TOML, to paste into `config.toml` | `webtuidb -theme >> theme.toml` |
| `-man` | print the man page (roff) | `webtuidb -man > webtuidb.1 && man ./webtuidb.1` |
| `-completion shell` | print a completion script for `bash`, `zsh` or `fish` | see below |
| `-list-connections` | print the saved connection names, one per line (the completion scripts use it) | `webtuidb -list-connections` |
| `-version` | print the version | `webtuidb -version` → `webtuidb v1.4.0` |
| `-changelog` | open the release notes in the browser and print their URL (`https://github.com/outofmemry/webtuidb/releases`); prints `could not open a browser: …` first when that fails | `webtuidb -changelog` |
| `-bugreport` | print version, OS, terminal, drivers in use and the config with secrets masked; sends nothing | `webtuidb -bugreport \| pbcopy` |
| `-check-update` | ask GitHub for the latest release (one request, 5 second limit; never automatic) | `webtuidb -check-update` |
| `-set-passcode` | read a passcode twice without echo and print the `[security] passcode` lines | `webtuidb -set-passcode >> ~/.config/webtuidb/config.toml` |
| `-theme-install url[@ref]` | `git clone --depth 1` a theme into the themes folder | `webtuidb -theme-install https://github.com/you/nord-webtuidb@v2` |
| `-theme-update` | `git pull --ff-only` every installed theme that is a git checkout | `webtuidb -theme-update` |

`-version`, `-man`, `-changelog`, `-bugreport`, `-check-update`, `-set-passcode`, `-theme-install`,
`-theme-update`, `-mcp-config` and `-completion` do not read `config.toml`, so they work when it is broken.

## Other flags

| Flag | Default | Does | Example |
| --- | --- | --- | --- |
| `-config path` | `~/.config/webtuidb/config.toml` | the config file to read. `connections.toml` is still read from the config directory | `webtuidb -config ./work.toml` |
| `-ssh-log path` | — | append an SSH tunnel debug log to the file (created 0600) | `webtuidb -ssh-log /tmp/ssh.log "Shop prod replica"` |
| `-listen addr` | `127.0.0.1:7788` | with `--frontend`: the address to serve on | `webtuidb --frontend --listen 127.0.0.1:8080` |
| `-open` | off | with `--frontend`: open the browser | `webtuidb --frontend --open` |
| `-token value` | random (or `WEBTUIDB_TOKEN`) | with `--frontend`: the API token | `webtuidb --frontend --token devtoken` |
| `-insecure-listen` | off | with `--frontend`: allow a non-loopback `--listen` address | `WEBTUIDB_PASSWORD=… webtuidb --frontend --insecure-listen --listen 0.0.0.0:7788` |

`-listen`, `-open`, `-token` and `-insecure-listen` without `--frontend` fail:
`--listen only works together with --frontend`. `--insecure-listen` with no token, no password and no
`WEBTUIDB_INSECURE=1` fails: `--insecure-listen without authentication: set WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or [web] password / --token), or WEBTUIDB_INSECURE=1 to accept an open server`.
See [Serving the web UI](../../web-ui/serving/) for the server's own rules.

### Messages of the one-shot commands

| Command | Says |
| --- | --- |
| `-check-update` | `a newer version is available: v1.5.0 (you have v1.4.0)` and the releases URL; `you are up to date (v1.4.0, latest v1.4.0)`; `latest release is v1.4.0; this build is "dev" and cannot be compared`; `No releases published yet.`; errors `check-update: …` |
| `-set-passcode` | prompts `New passcode:` and `Repeat it:` on standard error, then `Put this in ~/.config/webtuidb/config.toml:` and prints `[security]` / `passcode = "$2a$…"`. `the passcodes differ; nothing was changed`; without a terminal `set-passcode needs a terminal to read the passcode from` |
| `-theme-install` | `installed theme "nord-webtuidb"; set [theme] name = "nord-webtuidb"`. Refuses an installed name (`… is already installed (…); use -theme-update`) and a repository without a top-level `theme.toml` (nothing is kept). git runs without a credential prompt, for at most 2 minutes |
| `-theme-update` | one line per theme with git's output; `no git-installed themes in …` |
| `-completion tcsh` | `unknown shell "tcsh" (bash \| zsh \| fish)` |

### `-bugreport`

Paste it into an issue; nothing is sent anywhere. The config is decoded and printed again (comments are lost),
with the value of every key whose name contains `password`, `passcode`, `api_key`, `secret` or `token`, and every
`value` key, replaced by `***`, at any depth. Keys ending in `_cmd` are commands, not secrets, and stay. The
password part of any URL becomes `:***@`. A config that does not parse is masked line by line instead. An
example (shortened; the config held an `[ai.providers.openai]` entry and an MCP token):

```text
$ webtuidb -bugreport
webtuidb v0.0.0-20261029061057-945f583041c5+dirty
os/arch: darwin/arm64
go: go1.27.3
TERM: xterm-256color
drivers in use: cassandra, clickhouse, dynamodb, mariadb, mongodb, mysql, postgres, redis

--- /Users/you/.config/webtuidb/config.toml (secrets masked)
[ai]
  model = "claude-opus-5-5"
  provider = "anthropic"
  [ai.providers]
    [ai.providers.openai]
      api_key_cmd = "security find-generic-password -s openai -w"
      model = "gpt-4.1"
      provider = "openai"

[general]
  prefix = "C-d"

[mcp]
  enabled = true
  port = 51193

  [[mcp.token]]
    connections = ["Shop (Postgres)"]
    name = "claude-code"
    scope = "read-write"
    value = "***"
```

Without a config file the last part is `(no config file)`.

## Shell completion

The scripts complete the flags, the values of `-mcp-config` and `-completion`, file names for `-config` and
`-ssh-log`, and saved connection names for the argument (by running `webtuidb -list-connections`).

{{< tabs items="bash,zsh,fish" >}}
{{< tab >}}
```sh
# current shell
source <(webtuidb -completion bash)
# every new shell
webtuidb -completion bash > ~/.local/share/bash-completion/completions/webtuidb
```
bash inserts a connection name that contains spaces without quotes; quote it by hand.
{{< /tab >}}
{{< tab >}}
```sh
# a folder on $fpath, before compinit runs
webtuidb -completion zsh > "${fpath[1]}/_webtuidb"
```
{{< /tab >}}
{{< tab >}}
```sh
webtuidb -completion fish > ~/.config/fish/completions/webtuidb.fish
```
{{< /tab >}}
{{< /tabs >}}

## Environment variables

| Variable | Used for |
| --- | --- |
| `XDG_CONFIG_HOME` | config directory `$XDG_CONFIG_HOME/webtuidb` (`config.toml`, `connections.toml`, `plugins/`, `themes/`); default `~/.config`. Only an absolute path counts |
| `XDG_DATA_HOME` | data directory `$XDG_DATA_HOME/webtuidb` (history, saved queries, chats, boards, `state.json`); default `~/.local/share` |
| `HOME` | the defaults above; when it is unset and no absolute XDG path is set, webtuidb stops: `cannot find your home directory (HOME is unset); set HOME or XDG_CONFIG_HOME/XDG_DATA_HOME to absolute paths` |
| `WEBTUIDB_TOKEN` | with `--frontend`: the API token when `--token` is not given |
| `WEBTUIDB_PASSWORD` | with `--frontend`: the login password, over `[web] password` |
| `WEBTUIDB_INSECURE` | `1` lets `--insecure-listen` run with no authentication |
| `WEBTUIDB_DEV_ORIGIN` | comma separated origins (`http://localhost:5173`) the web API accepts besides its own, for front-end development |
| `WEBTUIDB_COLORS` | `16` forces the 16-colour palette; any other value forbids it (default: guessed from `COLORTERM` and `TERM`) |
| `WEBTUIDB_OSC52` | `1` lets the terminal UI copy through the terminal itself (an OSC 52 escape), which reaches your local clipboard from a remote shell; on by default over SSH (`SSH_TTY`, `SSH_CONNECTION` set) |
| `VISUAL`, `EDITOR` | the editor the settings screen opens `config.toml` in (default `vi`) |
| `SSH_AUTH_SOCK`, `USER` | the SSH agent for tunnels, and the default SSH user |
| `TERM`, `COLORTERM` | colour support; shown by `-bugreport` |

## Exit codes

| Code | When |
| --- | --- |
| `0` | success, including `-help` |
| `1` | any error; the message is printed as `webtuidb: …` on standard error |
| `2` | an unknown flag or a flag without its value (the usage is printed) |

At start webtuidb also prints warnings to standard error without stopping: a saved connection that cannot be
used (`webtuidb: connection "x" is unusable: …`), a `[keys]` entry that took another action's key, and a recovered
`state.json`.
