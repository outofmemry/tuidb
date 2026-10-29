---
title: Troubleshooting
description: Symptoms, causes and fixes for connection, TLS, SSH, configuration, web UI, terminal, clipboard and crash problems in webtuidb.
weight: 70
---

Each entry starts from what you see, quotes the message webtuidb prints (verified against the code), and gives the cause and the fix. Messages from the terminal UI appear in an error box or the status line; the web shows the same text in a notification or under the form. Startup errors are printed to standard error as `webtuidb: <message>` and webtuidb exits with status 1.

{{< ui tui="reference-troubleshooting-connect-failed" web="reference-troubleshooting-connect-failed" >}}A PostgreSQL URL pointing at a port where nothing listens (`127.0.0.1:55999`). The terminal shows the driver's full message in a "✗ Connection failed" box; the web shows it in red under the connection form after **Test**. The address appears twice because the default `ssl_mode = prefer` tries TLS first and then plain text; both attempts were refused. When the two lines differ, read the last one: it is the real cause.{{< /ui >}}

## Connecting

### Connection refused or nothing answering

```text
connect Shop: connect to postgres: failed to connect to `user=webtuidb database=shop`:
	127.0.0.1:55999 (127.0.0.1): dial error: dial tcp 127.0.0.1:55999: connect: connection refused
```

```text
connect Shop: connect to 127.0.0.1:55999: dial tcp 127.0.0.1:55999: connect: connection refused
```

**Cause:** nothing listens at that host and port: the server is stopped, the port is wrong, or the server only listens on another interface. `no such host` means the host name does not resolve; `i/o timeout` or `context deadline exceeded` usually means a firewall drops the packets, or the engine expects TLS (see below).

**Fix:** check the host and port in the connection (the URL form is `postgresql://user@host:port/db`), that the server is running (`docker ps`, `pg_isready`, `mysqladmin ping`), and that it listens on the address you use (`listen_addresses` in PostgreSQL, `bind-address` in MySQL). For a server inside Docker, publish the port (`-p 5432:5432`) and connect to `127.0.0.1`. The **Test** entry of the connection menu checks a connection without opening a workspace. The first attempt to connect gives up after 30 seconds.

Other messages you may meet at this step:

| Message | Cause | Fix |
|---|---|---|
| `password authentication failed for user "…" (SQLSTATE 28P01)` (PostgreSQL), `Access denied for user` (MySQL) | wrong user or password | edit the connection; `password = "ask"` asks every time |
| `sorry, too many clients already (SQLSTATE 53300)` | the PostgreSQL server reached `max_connections` | close other clients or raise `max_connections` |
| `password_cmd for "…" failed: …` | the `password_cmd` program failed or timed out (30 seconds) | run the command yourself in a shell |
| `connection "…" is unusable: …` (at startup) | an entry in `connections.toml` cannot be used, for example a driver that no longer exists | fix or remove that entry; the rest load normally |
| `no saved connection named "…" (and it is not a URL)` | the name given on the command line matches no saved connection | `webtuidb -list-connections` prints the names |

### SSL / TLS errors

`ssl_mode` takes `disable`, `prefer`, `require`, `verify-ca`, `verify-full` or `verify-identity`; each engine maps it to its own TLS options.

| Engine | What `prefer` does | Typical message | Fix |
|---|---|---|---|
| PostgreSQL, Redshift, Greenplum, Vertica | tries TLS, falls back to plain | `tls error: server refused TLS connection` with `require` on a server without TLS | use `prefer` or `disable`, or enable `ssl` on the server. `verify-identity` is refused: `ssl_mode "…" is not supported by PostgreSQL (use disable, prefer, require, verify-ca or verify-full)` |
| MySQL, MariaDB, SingleStore | tries TLS without checking the certificate, falls back to plain | `tls: failed to verify certificate: x509: … certificate is not standards compliant` with `verify-full` against a server's auto-generated certificate | use `require` (encrypted, unchecked), or give the server a proper certificate and set `ssl_ca` |
| ClickHouse, Redis, DynamoDB | plain text (no opportunistic TLS) | `context deadline exceeded` with `require` against a server without TLS | use `disable`/`prefer` for a plain server, or enable TLS on the server |
| MongoDB | plain text | `server selection error: context deadline exceeded, current topology: { … Last error: EOF }` with `require` against a plain server | as above |
| Cassandra | TLS without certificate checks (no fallback) | a handshake error against a plain server | use `disable` for a plain server |

Messages about certificate files: `ssl_ca …: no certificates found` (the file is not a PEM certificate), `ssl_cert/ssl_key: …` (the pair does not load), `iam_auth needs TLS: ssl_mode "…" would send the IAM token unencrypted (use require, verify-ca or verify-full)`. `verify-ca` checks the chain but not the host name; `verify-full` and `verify-identity` check both.

{{< callout type="info" >}}A URL pasted from another tool keeps its TLS meaning: `sslmode`, `ssl-mode`, `tls` and `ssl=true` are translated, and a value webtuidb does not understand is an error rather than a silent switch to plain text. `rediss://` means `require`.{{< /callout >}}

### SSH tunnel failures

| Message | Cause | Fix |
|---|---|---|
| `ssh: <host> is not in ~/.ssh/known_hosts; run \`ssh user@host\` once to trust it` | the bastion's host key is unknown; webtuidb never trusts a new key silently | run the command it prints once in a shell and accept the key |
| `ssh: no key, password or running ssh-agent to authenticate with` | no `ssh_key`, no SSH password and no `SSH_AUTH_SOCK` | add a key, a password, or start ssh-agent and `ssh-add` |
| `ssh key <path> is encrypted (add it to ssh-agent)` | a passphrase-protected key where no passphrase prompt is available | `ssh-add <path>`; the terminal UI asks for the passphrase itself |
| `ssh key: open …: no such file or directory` | wrong `ssh_key` path | fix the path (`~/` is expanded) |
| `ssh: cannot read ~/.ssh/known_hosts: …` | the file is missing or unreadable | `ssh` to the host once; it creates the file |
| `ssh: handshake failed: ssh: unable to authenticate …` | the server refused every method offered | check the SSH user, key and password |
| `proxy jump <hop>: …` | the ProxyJump bastion (from the connection or `~/.ssh/config`) failed; the rest of the message says how | fix the bastion's `~/.ssh/config` block, or set `proxy_jump = "none"` |

**Trace a tunnel:** start webtuidb with `--ssh-log FILE` (terminal or `--frontend`), or in the terminal choose "SSH log" in {{< key "C-d p" >}} to write `ssh.log` in the data folder. The log records each step (`ssh: dial host:22 as user`, `ssh: host:22 via bastion …`, `ssh: handshake with … failed: …`, `ssh: connected to …`). The web's "Enable SSH log" cannot switch it on from the page; restart the server with `--ssh-log`.

### "The server is taking too long"

A statement still running after `[general] query_timeout_secs` opens **The server is taking too long (over *N*s)** with *Keep waiting* and *Cancel the query*. Keep waiting asks again after another period; the prompt closes by itself when the statement finishes. `query_timeout_secs = 0` turns the prompt off. To stop every running statement, use `cancel_queries` ({{< key "C-d M-x" >}}); in the editor, `cancel_query` ({{< key "C-d ." >}}).

### Safe mode asks for a password the connection does not have

At Safe Mode 1 and Safe Mode 2, webtuidb asks for the database password before it sends a change, and compares it with the connection's stored password. When the connection has no stored password (`password = "ask"`, a `password_cmd` that returned nothing, or a database without one), there is nothing to compare against, so a blank field must not pass. Instead:

- the terminal shows a red confirmation, **Safe Mode 2: this connection has no stored password. Send?**, where only `y` sends;
- the web asks for a confirmation instead of a password.

A typed password that does not match fails with `wrong password`. To stop being asked, lower the level with `safe_mode` ({{< key "C-d S" >}}). See [Safety](../../safety/).

## Configuration

### config.toml rejected at startup

webtuidb refuses to start with a setting it does not understand, so a typo is never silently ignored. The message names the file or the setting:

```text
webtuidb: /home/you/.config/webtuidb/config.toml: unknown setting "general.row_limt"
webtuidb: /home/you/.config/webtuidb/config.toml: toml: line 2: expected '.' or ']' to end table name, but got '\n' instead
webtuidb: general.row_limit must be > 0
webtuidb: general.new_tab_opens: "x" is not one of query|empty
webtuidb: general.default_safe_mode: unknown safe mode "paranoid" (want silent|alert1|alert2|safe1|safe2)
webtuidb: [keys] unknown action "committ"
webtuidb: [keys] commit and filters are both bound to x in grid
webtuidb: [keys] commit: esc cancels the prefix and cannot be bound
webtuidb: security.passcode must be a bcrypt hash; make one with `webtuidb -set-passcode`
webtuidb: /home/you/.config/webtuidb/config.toml holds secrets and is readable by other users; chmod 600 it (…)
```

Fix the line it names. Print-only flags such as `-version`, `-bugreport` and `-man` work even with a broken file. `webtuidb -keys` shows the keymap the file produces. Settings are listed in the [settings reference](../../configuration/settings-reference/).

### connections.toml permissions

`connections.toml` holds passwords, so it must be readable by you only. When it is readable by group or others, webtuidb sets it to mode 600 at startup. When it cannot (another owner, a read-only mount such as a Docker bind mount):

```text
webtuidb: warning: could not chmod /…/connections.toml to 600 (operation not permitted); continuing
```

This warning appears only when the file is not readable by others anyway or its folder cannot be written. Otherwise startup stops with `/…/connections.toml is readable by other users and could not be fixed: …`. Fix it with `chmod 600 ~/.config/webtuidb/connections.toml` as the file's owner. Files in `[[source]]` folders are never changed. An unknown key in `connections.toml` stops startup with `unknown setting "…"`, like `config.toml`.

### state.json was not valid JSON

```text
webtuidb: /…/state.json was not valid JSON and was moved to /…/state.json.corrupt
```

webtuidb starts with empty UI state (pins, recent items, table filters, open tabs). Nothing else is lost; delete the `.corrupt` file when you no longer need it.

## Web UI

### The page says "This link needs the access token"

{{< ui web="reference-troubleshooting-no-token" >}}A web UI address opened without the access token.{{< /ui >}}

`webtuidb --frontend` prints its address once, with the token in the fragment: `http://127.0.0.1:7788/#token=…`. The page moves the token into the tab's `sessionStorage` and removes it from the address bar. A copied link, a new browser window, or a bookmark has no token. Links the app opens itself (open in a new browser tab) hand the token over.

**Fix:** open the printed address again, or start the server with a fixed token (`--token` or `WEBTUIDB_TOKEN`) or a login password (`[web] password` or `WEBTUIDB_PASSWORD`), which shows a **Sign in** page instead. In a private window, or with site storage blocked, the token lives only in memory and is lost on reload. After five wrong passwords in ten minutes logins are locked for a minute: `too many failed logins; wait a minute`.

### "origin not allowed" behind a reverse proxy

```text
origin not allowed (set WEBTUIDB_DEV_ORIGIN=http://host:port to allow a dev front end)
```

The server checks each request's `Origin` header against its own address, to stop other sites from calling the API. On a loopback `--listen` address (the default), the origin must be `localhost`, `127.0.0.1` or `[::1]` on the listen port, or listed in `WEBTUIDB_DEV_ORIGIN`. Behind a reverse proxy the browser's origin is the proxy's (`https://db.example.com`), so it does not match.

**Fix:** keep `--listen 127.0.0.1:7788`, proxy to it, and start the server with `WEBTUIDB_DEV_ORIGIN=https://db.example.com` (comma-separate several). With a non-loopback `--listen`, the origin must equal the server's scheme and the `Host` header, which a TLS-terminating proxy cannot satisfy; use the loopback setup. A cross-site request (`Sec-Fetch-Site: cross-site`) is always refused with `unauthorized`. For the Vite dev server, keep `changeOrigin: false` in the proxy.

### The server refuses to listen

```text
refusing to listen on "0.0.0.0:7788": not a loopback address (pass --insecure-listen to override; credentials will cross the network)
--insecure-listen without authentication: set WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or [web] password / --token), or WEBTUIDB_INSECURE=1 to accept an open server
--listen only works together with --frontend
```

A non-loopback address needs `--insecure-listen`, and then a token or password unless you set `WEBTUIDB_INSECURE=1`. The plain HTTP traffic, passwords included, crosses the network; set `[web] tls_cert` and `tls_key` to serve HTTPS.

### Docker refuses to start

The image runs `webtuidb --frontend --listen 0.0.0.0:7788 --insecure-listen`, so it stops with the `--insecure-listen without authentication` message above unless you pass a password or token:

```sh
docker run -p 127.0.0.1:7788:7788 -e WEBTUIDB_PASSWORD='choose-one' --user $(id -u) \
  -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb webtuidb
```

`--user $(id -u)` lets the container read your mounted `connections.toml` (mode 600). Without it you get the permissions error above, or the "could not chmod" warning on a read-only mount. Publish the port on `127.0.0.1` so only your machine reaches it. Inside the container, `127.0.0.1` is the container itself: use `host.docker.internal` (Docker Desktop) or the host's address for databases on your machine.

## Terminal

### Alt / Option keys do nothing on macOS

Bindings such as {{< key "C-d M-f" >}} (`find_replace`) need the terminal to send Option as Meta (Esc + key). By default, macOS terminals type a special character instead (`ƒ`).

- Terminal.app: Settings > Profiles > Keyboard > "Use Option as Meta key".
- iTerm2: Settings > Profiles > Keys > Left Option key: "Esc+".
- Ghostty: `macos-option-as-alt = true`. Kitty: `macos_option_as_alt yes`. Alacritty: `option_as_alt = "Both"`. WezTerm: `send_composed_key_when_left_alt_is_pressed = false`.

Or rebind the actions you use to keys without Alt under `[keys]`. In the browser, the web UI matches the physical key, so Option combinations work without settings.

### Ctrl+h/j/k/l do nothing inside tmux

The pane focus keys work without the prefix ({{< key "C-h" >}} {{< key "C-j" >}} {{< key "C-k" >}} {{< key "C-l" >}}). Inside tmux, a configuration such as vim-tmux-navigator binds the same keys with `bind -n`, so tmux keeps them. Some terminals also send {{< key "C-h" >}} as Backspace.

**Fix:** use the prefixed versions, {{< key "C-d M-h" >}} {{< key "C-d M-j" >}} {{< key "C-d M-k" >}} {{< key "C-d M-l" >}}, or rebind: `focus_left = "M-h"` under `[keys]` replaces both default keys of `focus_left` (prefix `-n ` for a key that needs no prefix, e.g. `"-n M-h"`). If your tmux prefix is also Ctrl+d, change webtuidb's: `[general] prefix = "C-a"`.

### Colours look wrong

webtuidb draws with 24-bit colours. When the terminal does not announce colour support (no `COLORTERM`, and a `TERM` such as `xterm` or `screen` without `256`, `truecolor` or a known terminal name), it falls back to the nearest of the 16 ANSI colours, which your terminal theme decides.

- Colours too plain: `export COLORTERM=truecolor`, or `WEBTUIDB_COLORS=truecolor`.
- Colours garbled (a terminal that claims more than it has): `WEBTUIDB_COLORS=16`.
- In tmux: `set -g default-terminal "tmux-256color"` and `set -as terminal-features ",*:RGB"`.

`webtuidb -theme` prints the resolved theme as `[theme.colors]` you can paste into `config.toml` and adjust. See [Themes](../../configuration/).

### Wide (CJK) text scrolls the cursor away

The terminal SQL editor counts characters, not screen cells, when it scrolls sideways, and CJK characters take two cells. On long lines of wide text the cursor can end up off screen. Turn on line wrap ({{< key "C-d M-g" >}}), or use the web editor. Filter fields and form fields measure by cell and are not affected.

### Copying does not reach my clipboard

The terminal copies three ways, in order:

1. Over SSH (`SSH_TTY` or `SSH_CONNECTION` set) or with `WEBTUIDB_OSC52=1`: an OSC 52 escape sequence, which your local terminal turns into a clipboard write.
2. Locally: the system tool, `pbcopy` (macOS), `wl-copy` (Wayland), `xclip` or `xsel` (X11), `clip` (Windows).
3. Otherwise OSC 52 again; the toast then says `copied … (via terminal, OSC 52)`, because it cannot be confirmed.

`Could not copy (no clipboard available)` means no tool was found and the terminal is known to ignore OSC 52 (Apple Terminal, `TERM=dumb`). Fixes: in tmux, `set -g set-clipboard on` (and `allow-passthrough on` for nested sessions); in iTerm2, enable "Applications in terminal may access clipboard"; on Linux, install `wl-clipboard` or `xclip`; over SSH from Apple Terminal, switch to a terminal that supports OSC 52. Some terminals drop OSC 52 copies near 100 KB: export large results instead.

## Crashes and bug reports

When webtuidb crashes it prints

```text
webtuidb: crashed; details were written to /…/.local/share/webtuidb/crash.log (nothing was sent anywhere)
```

and exits with status 2 after restoring the terminal. `crash.log` holds the time, the panic, the stack and the last 20 console lines; it rolls over to `crash.log.1` past 2 MB. In the web server, a panic in one request fails only that request with `internal error: … (see …/crash.log)`; the server keeps running. A panic inside a background command of the terminal UI ends the process without writing the log.

`webtuidb -bugreport` prints what an issue needs: version, OS and architecture, Go version, `$TERM`, the drivers your connections use, and your `config.toml` with secrets masked. It works even when the config does not load.

Report problems at [github.com/outofmemry/webtuidb/issues](https://github.com/outofmemry/webtuidb/issues) with the `-bugreport` output, the relevant part of `crash.log`, and the steps that lead to the problem. `webtuidb -check-update` tells you whether a newer release exists (it never checks by itself), and `webtuidb -changelog` opens the release notes.
