---
title: Serving the web UI
description: Start the browser front end, choose where it listens, protect it with a token, a password or TLS, run it in Docker or behind a reverse proxy, and know what it shares with the terminal.
weight: 10
---

`webtuidb --frontend` serves the browser app and a local HTTP API from the same binary and the same engine as the terminal UI. It is opt-in: without `--frontend` webtuidb starts the terminal UI and never opens a port.

{{< ui tui="web-ui-serving-start" web="web-ui-start-page" >}}
The terminal prints one line with the address and the access token; the browser opens on the connection list.
{{< /ui >}}

## Start it

```sh
webtuidb --frontend --open
# webtuidb web UI: http://127.0.0.1:7788/#token=3f2a9c0e5b7d41e8a6c1d09b2e4f7a33
```

Open the printed address in a browser, or pass `--open` and webtuidb opens it for you (`open` on macOS, `xdg-open` on Linux, the default handler on Windows). The part after `#token=` is the access token. The browser keeps it for this browser tab and removes it from the address bar, so the page you bookmark has no secret in it.

The terminal UI and `--frontend` are separate modes. You cannot combine `--frontend` with another mode flag (`--mcp`, `--keys`, `--version`, `--man` and so on) and it takes no connection name or URL argument. Stop the server with {{< key "C-c" >}}: open sessions are closed first, and the server waits up to three seconds for requests in flight.

## Flags

| Flag | Default | What it does |
|---|---|---|
| `--frontend` | off | Serve the web UI instead of starting the terminal UI. |
| `--listen addr` | `127.0.0.1:7788` | Address and port to listen on. A loopback address (`127.0.0.1`, `[::1]`, `localhost`) is always accepted. Any other address is refused unless you also pass `--insecure-listen`. |
| `--insecure-listen` | off | Allow a non-loopback `--listen` address, for example `0.0.0.0:7788`. Credentials and data then cross the network, so read [Reaching it from another machine](#reaching-it-from-another-machine). |
| `--open` | off | Open the printed address in your default browser. |
| `--token t` | random | The API token. Without it webtuidb makes 16 random bytes (32 hex characters) and prints them once. |
| `--config path` | `~/.config/webtuidb/config.toml` | The config file. The server also reads `connections.toml` from the same folder. |
| `--ssh-log file` | none | Write an SSH tunnel debug log. The browser cannot switch this on later, so you set it when the server starts. |

`--listen`, `--open`, `--token` and `--insecure-listen` without `--frontend` are an error: `--listen only works together with --frontend`.

If a port is taken, the server stops with the operating system's message (`bind: address already in use`). Pick another port with `--listen 127.0.0.1:7790`.

### Environment variables

| Variable | Same as | Notes |
|---|---|---|
| `WEBTUIDB_TOKEN` | `--token` | Used when `--token` is empty. Prefer it to the flag on a shared machine: a flag shows in the process list. |
| `WEBTUIDB_PASSWORD` | `[web] password` | Overrides the config value. Turns on the sign-in page, see [Sign-in page](#sign-in-page-web-password). |
| `WEBTUIDB_INSECURE` | none | Set to `1` to accept `--insecure-listen` with no token and no password. |
| `WEBTUIDB_DEV_ORIGIN` | none | Origins that may call the API besides the server's own (comma separated). For a front end served from another port, or a reverse proxy; see [Behind a reverse proxy](#behind-a-reverse-proxy). The server prints a warning at start when it is set. |

## The access token

Every request to the API (`/api/...`) needs the token in an `Authorization: Bearer` header. The web app sends it for you. The token is how the page proves it came from the address webtuidb printed, and it is what keeps another web page in your browser from calling a server that listens on `localhost`.

- The printed address carries the token in the URL **fragment** (after `#`). A browser never sends the fragment to a server, so it does not appear in any access log, proxy log or `Referer` header.
- The app copies the token into `sessionStorage` (key `webtuidb-token`) and replaces the address with the same page minus the fragment. A new browser tab starts with empty `sessionStorage`; the app hands the token over to tabs it opens itself, see [Deep links](../deep-links/#how-the-token-is-carried).
- A link copied from another tab has no token. Opening it in a fresh browser shows a short page saying the link needs the access token. Open the address webtuidb printed, then open the link again in the same browser.

{{< ui web="web-ui-token-needed" >}}
A link opened without a token: nothing else loads.
{{< /ui >}}

The token is compared in constant time. The token is not a user account: everyone who has it has the same access to every saved connection.

## Sign-in page (`[web] password`)

For a server other people open in their own browsers, you can give them a password instead of the token. Set it in `config.toml` or with `WEBTUIDB_PASSWORD`:

```toml
[web]
password = "choose-a-long-one"
```

With a password set, a browser that has no token sees a sign-in page. The server answers `POST /api/login` with an `HttpOnly`, `SameSite=Strict` cookie (`webtuidb_session`) that works in place of the token for 12 hours; the cookie is marked `Secure` when you serve TLS. The token printed at start keeps working too, so the address you were given still signs you straight in.

{{< ui web="web-ui-login" >}}
The sign-in page.
{{< /ui >}}

Wrong passwords are slowed and counted. Every wrong attempt waits 0.4 seconds before the answer; the fifth wrong attempt within ten minutes, from anyone, locks sign-in for 60 seconds (`too many failed logins; wait a minute`). The counter is global, not per address, so a stranger who can reach the page can also lock you out for a minute. Keep the server off the open internet.

{{< ui web="web-ui-login-wrong" >}}
A wrong password: the page says `wrong password` under the field.
{{< /ui >}}

{{< callout type="info" >}}
The password protects the web server only. The terminal UI has no login. A password in `config.toml` is a plain-text secret: webtuidb makes the file mode `0600` when it holds one. The settings editor never shows the value and cannot set it; edit the file or use the environment variable.
{{< /callout >}}

The passcode lock (`[security] passcode`) is a different thing: it hides the page in an unattended browser. It is covered under [Safety](../../safety/).

## Reaching it from another machine

The default is loopback only, so the server is reachable from the machine it runs on and nothing else. A `--listen` address that is not loopback is refused:

```
webtuidb: refusing to listen on "0.0.0.0:7788": not a loopback address (pass --insecure-listen to override; credentials will cross the network)
```

With `--insecure-listen` the server listens on that address, but it will not start without some form of authentication:

```
webtuidb: --insecure-listen without authentication: set WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or [web] password / --token), or WEBTUIDB_INSECURE=1 to accept an open server
```

Pick one of the three, or `WEBTUIDB_INSECURE=1` if you really want an open server. Over plain HTTP the server warns that the token, the password and your data cross the network unencrypted. Use one of these instead of plain HTTP on a network you do not control:

1. **TLS in webtuidb.** Set both `[web] tls_cert` and `[web] tls_key`. The server then serves HTTPS, prints an `https://` address and marks the sign-in cookie `Secure`. Setting only one of the two is an error at start. The browser's settings editor shows these two as read-only because they name files on the server.
2. **TLS in a reverse proxy** in front of a server that stays on loopback, see below.
3. **An SSH tunnel:** `ssh -L 7788:127.0.0.1:7788 server`, then open `http://127.0.0.1:7788/#token=…` locally.

```toml
[web]
tls_cert = "/etc/webtuidb/fullchain.pem"
tls_key  = "/etc/webtuidb/privkey.pem"
```

## Behind a reverse proxy

The API refuses a request whose `Origin` header does not match the server. A browser sends `Origin` on every request that changes something, so a proxy that serves the app under another address works only when the server knows that address. This is what stops a page on another site from using your token-less browser session against a server on your laptop (a DNS rebinding or cross-site request), and it is why the check is against the address the server was configured with, not against the `Host` header the client sent.

The rules, in the order the server applies them:

| Listening on | An `Origin` is accepted when |
|---|---|
| a loopback address (the default) | it is in `WEBTUIDB_DEV_ORIGIN`; or its scheme is the server's (`http`, or `https` when you set TLS) and it is the listen address itself, a loopback host (`localhost`, `127.0.0.1`, `[::1]`) on the listen port, or equal to the `Host` header of a loopback request. |
| any other address (`--insecure-listen`, Docker) | it is exactly `scheme://Host`, where `scheme` is `https` only if webtuidb itself serves TLS, and `Host` is the header of that request. `WEBTUIDB_DEV_ORIGIN` is not consulted. |
| any | a request with no `Origin` header (curl, scripts) always passes this check; it still needs the token. |

Requests the browser marks `Sec-Fetch-Site: cross-site` are always refused. A refused request is a `403` with `origin not allowed (set WEBTUIDB_DEV_ORIGIN=http://host:port to allow a dev front end)`.

For a proxy that ends TLS and talks plain HTTP to webtuidb on the same machine, keep webtuidb on loopback and name the public address:

```sh
WEBTUIDB_DEV_ORIGIN=https://db.example.com \
  webtuidb --frontend --listen 127.0.0.1:7788 --token "$(cat /etc/webtuidb/token)"
```

```nginx
location / {
    proxy_pass http://127.0.0.1:7788;
    proxy_set_header Host $host;
    proxy_http_version 1.1;
    proxy_buffering off;          # keeps the event streams (query results, job progress) live
    proxy_read_timeout 1h;        # long queries hold the request open
    client_max_body_size 600m;    # an import upload may be up to 512 MB
}
```

{{< callout type="warning" >}}
`WEBTUIDB_DEV_ORIGIN` trusts the origins you list with the same power as the server's own page. List only addresses you control. Behind a proxy, also add the proxy's own sign-in (or keep `[web] password` set): the proxy and the token are both open doors if either is left unprotected.
{{< /callout >}}

With the server on a non-loopback address behind a TLS-ending proxy the two schemes differ (`https` from the browser, `http` at the server), so every write is refused. Either keep the server on loopback as above, or give the server a certificate and let the proxy talk HTTPS to it.

Event streams (`/api/sessions/{id}/exec/stream` and job events) send `X-Accel-Buffering: no`. A proxy that buffers them still works: the app falls back to polling a job every 500 ms, and a query result simply arrives at the end.

## What the web UI shares with the terminal UI

Both front ends use the same files, so you can open a connection in the terminal and in the browser at the same time and see the same things.

| Shared | Where it lives |
|---|---|
| Saved connections, groups, tags, colours, safe mode per connection | `connections.toml` in the config folder. The server re-reads it whenever the file changes and checks it before saving, so a save from a stale browser tab fails with "the connection list changed elsewhere" instead of overwriting yours. |
| Settings and key bindings | `config.toml`. Saving from the browser rewrites the file (comments in it are not kept, and the app says so). A saved setting reaches the browser on its next load; the server keeps the settings it started with for most of its own behaviour (limits, timeouts), so restart it after changing those. |
| Query history | `history/<connection>.jsonl` in the data folder. |
| Saved queries | `queries/**/*.sql`, and any `[queries] shared_dirs` (read-only). |
| Metrics boards, plugins | `boards/*.toml` in the data folder, `plugins/` in the config folder. |
| Pinned and recent items, per-table filters, sort, hidden columns and column widths, sidebar groups, last-connected times, connection sort order, recent files, backup dialog options, open tabs (`auto_save_tabs`) and editor folds | `state.json` in the data folder. |
| Backups | `backups/` in the data folder. |

The two programs take a file lock when they read and write the same file, so saving from both at once is safe.

**Stays with the process.** The console log is kept in memory per server (the last 1000 lines, newest last) and is not the terminal's console. The web server's open sessions, the cached results behind editable grids and background jobs (imports, exports, searches) disappear when it stops.

**Stays in the browser.** The browser stores a few things for itself, because they are about one browser and not about the database:

| Storage | Keys | Holds |
|---|---|---|
| `sessionStorage` (one browser tab) | `webtuidb-token`, `webtuidb-open`, `webtuidb-tabs-<session>`, `webtuidb-reopened`, `webtuidb-ask-db` | The access token, which workspaces this browser tab has open, each workspace's tabs and unsent editor text, and two one-shot flags. A reload comes back to the same tabs; closing the browser tab forgets them. |
| `localStorage` (the browser) | `webtuidb-last`, `webtuidb-recent-files`, `webtuidb-handoff` | The workspaces to reopen next time, the names of `.sql` files you picked, and the 15-second token hand-off to a tab the app opens. |
| `localStorage` | `webtuidb-er:*`, `webtuidb-er-colors:*`, `webtuidb-er-view`, `webtuidb-board:*`, `webtuidb-objcolors:*`, `webtuidb-folds:*` | ER diagram box positions and colours, the board panel arrangement, sidebar item colours, and fold markers by statement. |
| `localStorage` | `webtuidb-editor-h`, `webtuidb-wrap`, `webtuidb-vim`, `webtuidb-invisibles`, `webtuidb-mongo-syntax`, `webtuidb-ai-model` | Editor height, word wrap, Vim keys, invisible characters, the Mongo syntax toggle and the last assistant model. |

A different browser, or the same address on a different port, starts empty for all of these. Private windows and blocked storage are tolerated: the app works, and these things last until reload.

## Sessions, tabs and several browsers

Connecting from the browser opens a **session**: a server-side connection pool with its own safe mode level, caches and background jobs. The browser refers to it by an id in the address (`/s/<id>`).

- Each browser tab keeps its own list of open sessions, so two browser tabs can have different workspaces open, or the same connection twice.
- Opening a workspace in a second browser tab (tab menu, **Open workspace in a new browser tab**) shows the same session in both. They share one pool and one set of caches; each tab has its own open table and editor tabs.
- Disconnecting closes the session on the server. Closing the browser tab does not: the server keeps it for an hour of no requests and then closes it (a running job keeps it alive). A session also stops if the server restarts, and the page then says "This session has ended".
- At most 32 sessions are open per server. The 33rd connect fails with `too many open sessions (32); disconnect one first`.
- With `[general] keep_alive` (the default) every session pings its database every 30 seconds.

## Docker

The repository's `Dockerfile` builds the web app and the binary into a distroless image that runs `webtuidb --frontend --listen 0.0.0.0:7788 --insecure-listen`. Because it listens on all addresses over plain HTTP, the container **refuses to start** unless `WEBTUIDB_PASSWORD` or `WEBTUIDB_TOKEN` is set (or you accept an open server with `WEBTUIDB_INSECURE=1`).

```sh
docker build -t webtuidb .
docker run --rm -p 127.0.0.1:7788:7788 \
  -e WEBTUIDB_PASSWORD='choose-a-long-one' \
  --user "$(id -u)" \
  -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb \
  -v ~/.local/share/webtuidb:/home/nonroot/.local/share/webtuidb \
  webtuidb
```

| Part | Why |
|---|---|
| `-p 127.0.0.1:7788:7788` | Publish on the host's loopback only. Without `127.0.0.1:` Docker publishes on every host address. Put TLS in front before you publish further. |
| `-e WEBTUIDB_PASSWORD=…` | The sign-in password. `WEBTUIDB_TOKEN` works too; the container still prints a random token at start. |
| `--user "$(id -u)"` | The image runs as the `nonroot` user by default and could not read or `chmod` files in your bind-mounted folders, which `webtuidb` keeps at mode `0600`. Running as your own user fixes that. |
| the first `-v` | Your `config.toml` and `connections.toml`, so the container sees your connections. |
| the second `-v` | History, saved queries and `state.json`. Without it they live in the container and vanish with it. |

The address the container prints (`http://0.0.0.0:7788/#token=…`) is the listen address; open `http://127.0.0.1:7788/` and sign in with the password, or replace `0.0.0.0` with `127.0.0.1` in the printed address to use the token. The container reaches databases on the host as `host.docker.internal` (Docker Desktop) or the host's address, not `127.0.0.1`: edit the saved connection's host accordingly. `make docker` builds the image and sets the version from `git describe`.

## Reference: ports and files the server touches

| Port | Opened when | Notes |
|---|---|---|
| `--listen` (default 7788) | `--frontend` | The web app and `/api`. |
| `[mcp] listen` or `127.0.0.1:[mcp] port` | `--frontend` and `[mcp] enabled = true` | A separate listener for MCP clients; it has its own tokens. The web app's **MCP activity** dialog and approval prompts read from it. |

## Settings

These `config.toml` keys change how the web UI is served or looks. The full list is in the [configuration reference](../../configuration/).

| Key | Default | Meaning |
|---|---|---|
| `[web] password` | unset | Sign-in password; overridden by `WEBTUIDB_PASSWORD`. |
| `[web] tls_cert`, `[web] tls_key` | unset | Serve HTTPS; set both or neither. |
| `[web] font_size` | `0` | Page font size in px (`0` = the built-in size). |
| `[web] line_height` | empty | CSS `line-height` value. |
| `[web] notifications` | `false` | Desktop notification when a backup or import finishes (the browser asks permission when you start one). |
| `[web] auto_hide_scrollbars` | `true` | Scrollbars show only while scrolling or hovering. |
| `[web] toolbar` | `[]` | Buttons on the top bar, see [The browser app](../browser-app/#the-top-bar). |
| `[general] reopen_workspaces` | `true` | Reopen the workspaces that were open last time, once per browser tab. |

## See also

- [The browser app](../browser-app/): start page, workspace, menus, keys.
- [HTTP API](../api/): the endpoints behind it.
- [Safety](../../safety/): safe mode, read-only connections, the passcode lock.
