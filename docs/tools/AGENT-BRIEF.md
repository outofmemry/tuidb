# Shared brief for documentation agents

You are writing one section of the webtuidb documentation. Read `docs/WRITING.md` first (style, page
pattern, shortcodes, screenshot rules) and `docs/tools/README.md` (tooling). This file has the
practical details.

## Ground truth

- The code is the source of truth: Go in `internal/` (terminal UI `internal/ui`, web server
  `internal/web`, engine `internal/db`, config `internal/config`, keymap `internal/keymap`, menus
  `internal/menus`, dialogs `internal/menus/dialogs.go`) and the web app in `web/src`. Read the handlers,
  the dialog definitions, the help text and the tests for your area before writing a sentence about it.
- `README.md` and the old plan (copied to the path given in your task) list features and the shortcuts
  taken in code (`// shortcut:` comments). Verify each claim in the code; the plan is a map, not proof.
- `docs/data/keymap.json` is the resolved keymap (action → key, context, description); `docs/data/menus.json`
  the context menus; `docs/data/meta.json` drivers, safe modes, operators and more. Quote keys from there.
- `./webtuidb -keys` and `-help` print the live bindings and flags.

## Environment (already running)

- Binary: `./webtuidb` in the repo root (built with the web app embedded).
- Test databases are up (Docker compose project `webtuidb-test`). Demo data: database `shop` on Postgres
  (`127.0.0.1:55432`, user/password `webtuidb`) with `categories`, `products`, `customers`, `orders`,
  `order_items`, `payments`, `reviews`, `audit_log`, `no_pk`, schema `analytics.daily_sales`, views
  `paid_orders`, `customer_summary`, matview `top_products`, functions, a trigger, a procedure, an enum
  `order_status`, sequence `invoice_seq`. MySQL `shop` (`127.0.0.1:53306`, root/root) has a smaller copy;
  Redis (56379, password `webtuidb`) has `shop:*` keys; MongoDB `shop` (57017, webtuidb/webtuidb,
  authSource admin) has `customers`, `products`, `orders`; MariaDB 53307, ClickHouse 59000, Cassandra 59042,
  DynamoDB local 58000 hold the compose seed only.
- Docs connections (names you pass to `-c`): "Shop (Postgres)" (blue, development), "Shop prod replica"
  (red, production, read_only, safe_mode alert2), "Shop (MySQL)", "Shop (MariaDB)", "Analytics
  (ClickHouse)", "Cache (Redis)", "Shop (MongoDB)", "Events (Cassandra)", "Sessions (DynamoDB local)".
  Defined in `docs/tools/env/config/webtuidb/connections.toml`; saved queries in
  `docs/tools/env/data/webtuidb/queries/`.
- Web UI for screenshots: `http://127.0.0.1:7797/#token=devtoken` (started with `docs/tools/serve.sh`).
  If it is not answering, start it yourself in the background: `nohup docs/tools/serve.sh >/dev/null 2>&1 &`.

## Taking screenshots

Terminal:
```
docs/tools/tui-shot.sh -o docs/static/img/tui/<stem>.png -c "Shop (Postgres)" -- sleep:2 j j Enter sleep:1 C-d f
```
Keys are tmux names (`C-d`, `M-f`, `S-Tab`, `Enter`, `Escape`, `Tab`, `Up`, `BSpace`, letters); `sleep:N`
waits, `type:text` types literally. The script prints the plain-text screen: read it and confirm the state is
the one you want before moving on. `-s 110x30` for a dialog-only shot, `-k` keeps the tmux session for a
look (`command tmux -L <socket> attach`), `-a "-keys"` passes flags to the binary instead of a connection.
Each run starts a fresh process, so every shot replays its key path from the start; keep paths short and
put a `sleep:2` after connecting and after anything that loads.

Web:
```
node docs/tools/web-shot.mjs --out docs/static/img/web/<stem>.png --steps '[{"click":"text=Shop (Postgres)"},{"wait":1500},{"click":"text=orders"},{"wait":1200},{"keys":["Control+d","f"]},{"wait":400}]' --text
```
Steps: `goto`, `click`, `dblclick`, `rightclick`, `hover`, `fill`, `type`, `press`, `keys`, `wait`,
`waitFor`, `scroll`, `eval`, `shot`, `viewport`; `--light`, `--mobile`, `--full`, `--clip x,y,w,h`,
`--script file.mjs` for anything the step list cannot express (`export default async (page, {shot}) => {}`).
`--text` prints the page text so you can confirm the state. A failed run writes `<stem>.failed.png`.
If a run fails with "browser not installed", wait a minute and retry (Chromium may still be downloading).

Rules:
- Use the demo data so screenshots look like a real project. Prefer `orders`, `customers`, `products`.
- Never commit destructive changes to the demo databases. Showing pending edits, previews and dialogs is
  fine as long as you discard or cancel. If a page needs committed changes (import, new table, user
  management…), create objects with a name prefixed by your section (`docs_<section>_…`) and drop them when
  done, or use a scratch database you create (`shop_<section>`).
- Other agents share the server, the databases and the data directory concurrently. Do not stop the web
  server, do not re-seed, do not edit `docs/tools/env/config`, do not delete another section's files.
- Put every screenshot you reference in place before finishing; no dangling `{{< ui >}}` references.
- Name files `<section>-<feature>[-<state>].png` with the section slug you were given.

## Deliverables

- Pages under `docs/content/docs/<section>/` (replace the placeholder `_index.md` with a real intro plus a
  `{{< cards >}}` grid of the section's pages, keep its `title` and `weight`).
- Screenshots under `docs/static/img/tui/` and `docs/static/img/web/`.
- Run `hugo -s docs --quiet` at the end and fix every warning or error that names your files.
- Finish with a short report: pages written, screenshots taken, anything you could not capture or verify
  (say exactly why), and any product bug you noticed along the way (do not fix the product).

Do everything yourself; do not spawn further agents.
