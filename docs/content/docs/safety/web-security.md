---
title: Web security
description: "How webtuidb --frontend keeps the browser UI to you: the access token, the optional password login, TLS, loopback-only listening, origin checks, Docker and deep links."
weight: 40
---

`webtuidb --frontend` serves the web UI and an HTTP API that can do everything the terminal can, with your
saved passwords. It is built to be reachable only by you: it listens on the loopback address, every API call
needs a secret token, and requests from other web sites are refused. This page explains each layer and what
you must add before you expose it to a network. The terminal UI opens no port, so none of this applies to it.

{{< ui web="safety-web-login" >}}With `[web] password` set, the page asks for it first. A wrong password shows "wrong password".{{< /ui >}}

## The access token

On start the server prints one line:

```
webtuidb web UI: http://127.0.0.1:7788/#token=3f9c…
```

- The token is 32 random hex characters, new on every start, unless you fix it with `--token <value>` or the
  environment variable `WEBTUIDB_TOKEN` (the flag wins over the variable).
- It sits after `#`, in the URL fragment. Browsers never send the fragment to a server, so it does not reach
  access logs or proxies.
- When the page loads it moves the token into the tab's `sessionStorage` (key `webtuidb-token`) and removes it
  from the address bar, so it is not in your history or in a screenshot of the URL. If storage is blocked
  (some private modes), the token lives in memory until you reload.
- Every API call sends it as `Authorization: Bearer <token>`. The server compares it in constant time.
- `--open` opens the printed address in your default browser.

### New tabs and windows

`sessionStorage` belongs to one browser tab, so a new tab starts without the token:

- **Copy link** in the tab menu copies a plain address without the token on purpose: the fragment would hand
  the API to whoever you paste the link to.
- **Open workspace in a new browser tab** in the tab menu hands the token to the new tab through `localStorage` (key
  `webtuidb-handoff`): it is written just before the tab opens, read and deleted by the new tab, and ignored if
  it is older than 15 seconds. The token never shows in that tab's URL. If storage is blocked, the address gets
  the `#token=` fragment instead.
- An address typed or pasted into a fresh tab without the token shows "This link needs the access token" with
  "Open the address that `webtuidb --frontend` printed, then open this link in the same browser."

{{< ui web="safety-web-no-token" >}}A deep link opened in a tab that has no token.{{< /ui >}}

### Deep links

The web UI has shareable addresses for a table (`/s/<session>/t/<schema>/<table>?filter=…`) and a saved query
(`/s/<session>/q/<path>`). They contain no secret: they work only in a browser tab that already has the token, or
with the token added as a fragment. A `filter` that does not parse is ignored rather than breaking the page. See
[Deep links](../../web-ui/deep-links/).

## Password login

Set a password to put a sign-in page in front of the token, for example when the token would end up in a
shared terminal or a container log:

```toml
[web]
password = "a long passphrase"
```

or `WEBTUIDB_PASSWORD=…` in the environment, which overrides `[web] password`. The settings screen never shows or
edits `web.password`.

- The page shows **Sign in** with a **Password** field. The button stays disabled until you type.
- A right password returns a login cookie, `webtuidb_session`: a random value the server keeps in memory, valid for
  12 hours, `HttpOnly` (scripts cannot read it), `SameSite=Strict` (other sites cannot send it), `Path=/`, and
  `Secure` when TLS is on. Browsers scope cookies by host name, not by port, so other local servers on the same
  host name receive it too; it is useless to them because only this server's memory knows the value.
- The token keeps working next to the password: the printed `#token=` address signs in without the form.
- A wrong password answers "wrong password" after a 0.4 second pause.
- After 5 wrong passwords within 10 minutes, every login is refused for 60 seconds with "too many failed logins;
  wait a minute". The counter is shared by all clients (marked as a known shortcut in the code), so someone who
  can reach the port can also keep you out for a minute at a time.
- Without a password set, `POST /api/login` answers "login is not enabled ([web] password is not set)".

There is no **Log out** button. A session ends when:

- you close the browser tab (the token in `sessionStorage` goes with it; with a password, the cookie remains
  until it expires);
- 12 hours pass (the login cookie);
- the server restarts (all login cookies are forgotten, and a random token changes).

Separately, a database session that no browser has used for an hour is closed on the server.

## TLS

```toml
[web]
tls_cert = "/path/to/cert.pem"
tls_key  = "/path/to/key.pem"
```

With both set the server speaks HTTPS and prints an `https://` address. Setting only one stops the start with
"[web] tls_cert and tls_key must be set together". Use TLS whenever the server is reachable from another machine;
on plain HTTP the token, the login password and your data cross the network unencrypted.

## Where it listens

| `--listen` | Result |
|---|---|
| default `127.0.0.1:7788` | loopback only: only programs on this machine can connect |
| `localhost:…`, `[::1]:…`, any `127.x.x.x` | loopback, allowed |
| anything else (`0.0.0.0:7788`, a LAN address) | refused: `refusing to listen on "0.0.0.0:7788": not a loopback address (pass --insecure-listen to override; credentials will cross the network)` |

With `--insecure-listen` a non-loopback address is accepted, but only with authentication you chose yourself:
without `--token`, `WEBTUIDB_TOKEN`, `[web] password` or `WEBTUIDB_PASSWORD` the start fails with
`--insecure-listen without authentication: set WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or [web] password / --token), or WEBTUIDB_INSECURE=1 to accept an open server`.
`WEBTUIDB_INSECURE=1` accepts the random token alone. Without TLS on a non-loopback address the server also prints
"WARNING: serving plain HTTP on …; the token, any login password and your data cross the network unencrypted
(set [web] tls_cert and tls_key)".

`--listen`, `--open`, `--token` and `--insecure-listen` only make sense with `--frontend`; given alone they stop
with "… only works together with --frontend".

## Requests from other sites

A page you visit in the same browser could try to call `http://127.0.0.1:7788/api/…`. The server refuses such
requests before it looks at the token:

- **Origin.** A request that carries an `Origin` header must come from the server itself. On a loopback listener
  the origin must use the same scheme and be the listen address, a loopback host (`localhost`, `127.0.0.1`,
  `::1`) on the listen port, or a loopback origin equal to the `Host` header. The check is made against the
  configured listen address, not only the `Host` header, so a DNS-rebinding attack (an evil name that resolves to
  127.0.0.1) does not pass. On a non-loopback listener the origin must equal `scheme://Host`.
- **Sec-Fetch-Site.** A request the browser marks `cross-site` is refused.
- **Content type.** Request bodies must be `application/json`; a form post from another site gets "Content-Type
  must be application/json" (HTTP 415).

A refused origin gets HTTP 403 "origin not allowed (set WEBTUIDB_DEV_ORIGIN=http://host:port to allow a dev front
end)". A missing or wrong token gets HTTP 401 "unauthorized". API answers are sent with `Cache-Control: no-store`.

`WEBTUIDB_DEV_ORIGIN` (comma-separated origins, such as `http://localhost:5173`) allows a front end served from
another port during development; the token is still required, and the server prints a warning while it is set.
See [Development](../../web-ui/development/).

## Docker

The image runs `webtuidb --frontend --listen 0.0.0.0:7788 --insecure-listen`, because inside a container the
port must be reachable from outside. The rules above therefore make it refuse to start unless you pass
`WEBTUIDB_PASSWORD` or `WEBTUIDB_TOKEN` (or explicitly `WEBTUIDB_INSECURE=1`):

```sh
docker run -p 127.0.0.1:7788:7788 -e WEBTUIDB_PASSWORD=… --user $(id -u) \
  -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb webtuidb
```

Publish the port on `127.0.0.1` as shown, so only your machine reaches it. The image serves plain HTTP; put TLS in
front of it, or mount a certificate and set `tls_cert` / `tls_key`, before you publish it anywhere else.

## Settings

| Setting | Where | Meaning |
|---|---|---|
| `password` | `[web]` in `config.toml`, or `WEBTUIDB_PASSWORD` | sign-in page in front of the token |
| `tls_cert`, `tls_key` | `[web]` | serve HTTPS |
| `--token`, `WEBTUIDB_TOKEN` | flag / environment | fixed token instead of a random one |
| `--listen` | flag | address, default `127.0.0.1:7788` |
| `--insecure-listen`, `WEBTUIDB_INSECURE=1` | flag / environment | allow a non-loopback address; accept it without a password or fixed token |
| `WEBTUIDB_DEV_ORIGIN` | environment | extra allowed origins for development |

`config.toml` is set to mode `0600` when it holds `[web] password`. See [Serving the web UI](../../web-ui/serving/)
and the [configuration reference](../../configuration/).

## Details and limits

- Safe mode is checked again on the server for every write, so a script calling the API meets the same
  confirmations and password prompts as the page (see [Safe mode](../safe-mode/)).
- Anyone who has the token has full access. Treat the printed line like a password: do not paste it into chats or
  tickets, and prefer `[web] password` when the start-up output is logged.
- The passcode lock is a screen lock in the browser, not an API lock; see [Passcode lock](../passcode-lock/).
