---
title: Development
description: Run the web app against a live API with hot reload, build it into the binary, run its tests, and the design rules in brief.
weight: 60
---

The web app lives in `web/`, the server that embeds and serves it in `internal/web/`. This page covers the web side. The Go side (engine, drivers, tests, the keymap) is in [Development](../../development/).

## The stack

| Piece | Used for |
|---|---|
| Vite and `@vitejs/plugin-react` | Dev server and production build. |
| React 19 and TypeScript | The components. |
| TanStack Router | The routes: `/`, `/s/$sessionId`, the deep links and `/sql` (`web/src/main.tsx`). |
| TanStack Query | Every API call and its cache. |
| TanStack Table and Virtual | The data grid (large results scroll without rendering every row). |
| CodeMirror 6 (`@uiw/react-codemirror`, `@codemirror/lang-sql`, `@replit/codemirror-vim`) | The SQL editor and read-only viewers. |
| Tailwind CSS v4 (`@tailwindcss/vite`) | Styling, as utility classes in the components. |
| Martian Mono, Bricolage Grotesque, simple-icons | Fonts and engine marks, bundled (nothing loads from a CDN). |

There is no component library and no state manager: pages talk to the API (`web/src/utils/api.ts`), and small pure modules under `web/src/utils/` hold the logic that is tested without a browser.

## Run it with hot reload

Start a server with a token you know, then the dev server:

```sh
webtuidb --frontend --token dev          # the API on 127.0.0.1:7788
cd web && npm install && npm run dev     # the app on http://localhost:5173
```

Open `http://localhost:5173/#token=dev`. The dev server proxies `/api` to `http://127.0.0.1:7788`; `WEBTUIDB_API` points it elsewhere:

```sh
WEBTUIDB_API=http://127.0.0.1:7797 npm run dev
```

Two settings in `web/vite.config.ts` are deliberate:

- `changeOrigin: false`. The proxy must not rewrite the `Host` header, because the API checks `Origin` against `Host`: the browser sends `Origin: http://localhost:5173`, the proxy keeps `Host: localhost:5173`, and the two match on a loopback address. With `changeOrigin: true` the `Host` becomes `127.0.0.1:7788` and every write is refused with `403 origin not allowed`.
- The build output goes to `../internal/web/dist`, the folder the Go binary embeds.

If you open the dev server by another name (a LAN address, a container), the `Origin` is no longer loopback. Tell the API with `WEBTUIDB_DEV_ORIGIN`, a comma-separated list of origins that may call it:

```sh
WEBTUIDB_DEV_ORIGIN=http://192.168.1.20:5173 webtuidb --frontend --token dev
```

The server prints a warning at start while it is set. Unset it outside development. See [Behind a reverse proxy](../serving/#behind-a-reverse-proxy) for the rules.

## Build it into the binary

```sh
make web build        # npm ci, tsc --noEmit, vite build into internal/web/dist, then go build -o webtuidb .
```

`make web` runs `npm ci && npm run build` (type-check, then `vite build`) and touches `internal/web/dist/.gitkeep`; `make build` is the plain `go build`. `internal/web/server.go` embeds the folder with `//go:embed all:dist`; the built files are git-ignored except `.gitkeep`, so a fresh checkout compiles without the app. A binary built that way works, but `/` answers `503 web app not built: run make web and rebuild` and the server says at start that the app is not built into this binary (the API still works). `go install` builds without the web app for the same reason. Node 20 or newer is needed (the Dockerfile uses 22).

## Tests

```sh
make web-test               # cd web && npx tsc --noEmit -p . && node --test tests/
cd web && npm run test:keys # only the key state machine
```

The tests in `web/tests/*.test.mjs` run under Node's own test runner against the pure modules (keys, links, workspaces, menus, pending changes, grid selection, imports, boards, and so on); nothing starts a browser. `internal/web/*_test.go` covers the server side with a fake driver, and `make parity` checks that both front ends expose the same menus, actions and settings.

## Shortcuts and the browser

A new binding has to land in both UIs in the same change: the action table in `internal/keymap/action.go`, the global `switch` in `act.current` in `web/src/routes/Workspace.tsx` (or the tab that answers the `webtuidb:action` event), and the name in `SUPPORTED`, which decides what the help dialog greys out. `GET /api/meta` carries the resolved keymap, and `web/src/utils/keys.ts` runs the same state machine as `internal/keymap`.

## Design rules in brief

The full notes are in `web/DESIGN.md`. In short:

- **The connection's colour is the only accent** in a workspace. Edited cells are amber, deleted rows coral, new rows green, whatever the accent.
- **No cards, gradients or shadows inside the page.** Hairlines, 2 px corners, outline buttons. Dark is the default; light is cool slate.
- **Utilities on the element.** Do not add semantic classes to `styles.css`; it holds the palette tokens and a handful of shared utilities (`btn-primary`, `btn-ghost`, `btn-danger`, `btn-on`, `btn-icon`, `popover`, `menu-item`, `chip`, `toolbar`).
- **Every floating menu goes through `Layer`**; never write your own full-screen backdrop.
- **Plain words first.** A failure says what to check in one sentence and keeps the driver's text behind **Technical details**. Anything that loses work asks first.
- **Quality floor:** visible keyboard focus, reduced motion respected, usable at phone width, sentence case, no all-caps labels.

## See also

- [Development](../../development/): the Go side, the test databases and the release build.
- [HTTP API](../api/): what the app calls.
