# webtuidb

One Go engine (`internal/db`, `internal/store`, `internal/config`, `internal/safemode`) with two
front ends: the terminal UI (`internal/ui`, Bubble Tea) and the web UI (`web/`, served by
`internal/web` when run with `--frontend`). Both must behave the same.

## Shortcuts apply to both UIs

Whenever a request mentions a shortcut, key binding, keymap, hotkey, prefix key, or "when I press X",
change **both** the terminal UI and the web UI in the same pass, even if only one is named.

- **Terminal UI:** bindings are the `defaults` / `noPrefixDefaults` tables in
  `internal/keymap/action.go` (tmux-style: `prefix` then key; `prefix` comes from config
  `[general] prefix`, overrides from `[keys]`). The action name is what the UI dispatches on; the
  `Desc` text is what the help screen shows. Add a test in `internal/keymap/keymap_test.go`.
- **Web UI:** the same action, same key, same prefix behaviour. `GET /api/meta` returns the resolved
  keymap (`keymap.prefix`, `keymap.bindings`), and `web/src/utils/keys.ts` runs the same state machine.
  Global actions are the `switch` in `act.current` in `web/src/routes/Workspace.tsx`; tab-level ones are
  answered by the active tab through the `webtuidb:action` window event (`keyActions` in
  `web/src/components/DataTab.tsx`, `acts` in `web/src/components/QueryTab.tsx`, the sidebar and filter bar). Every action
  the web implements must be in `SUPPORTED` in `Workspace.tsx`, or the help screen greys it out. The
  focused context comes from the nearest `data-ctx` attribute.
- Keep the two help listings identical (TUI `?` and the web help dialog): same action names, same
  descriptions, same order.
- A shortcut that cannot work in one UI (for example a terminal key the browser swallows) is not
  skipped silently: say so, and pick the closest key for that UI.

## Dev

- `make web` builds the web app into `internal/web/dist`; `go build` embeds it.
- `web/` dev: `npm run dev` proxies `/api` to `127.0.0.1:7788` (override with `WEBTUIDB_API`). The
  proxy must keep `changeOrigin: false` because the server checks `Origin` against `Host`.
- Design rules for the web UI are in `web/DESIGN.md`.
