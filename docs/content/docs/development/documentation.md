---
title: How these docs are built
description: Hugo and Hextra, the writing conventions, the screenshot tooling and its isolated environment, how to add a page or regenerate a screenshot, the search index and the Pages workflow.
weight: 50
---

This site lives in `docs/` of the repository: Markdown pages, a few shortcodes, a folder of screenshots and a set of scripts that take the screenshots from the real application. Nothing in it is generated from a mock-up; the key bindings, menus and driver lists are exported from a running server.

## The site

- **Generator and theme.** [Hugo](https://gohugo.io) (extended, 0.169.0 is what the workflow pins) with the [Hextra](https://imfing.github.io/hextra/) theme (v0.13.0). Hextra is a Hugo module imported in `docs/hugo.yaml` (`module.imports: github.com/imfing/hextra`) and **vendored** in `docs/_vendor` (with `_vendor/modules.txt`), so a build needs no network and no Go module download. `docs/go.mod` and `go.sum` pin the version.
- **Layout.** `docs/content/docs/<section>/<page>.md`, one folder per section (Getting started, Connections, Workspace … Development), each with an `_index.md` that holds the section's intro and a `{{</* cards */>}}` grid. The section order comes from the `weight` in each `_index.md`; the page order inside a section from each page's `weight` (10, 20, 30 …).
- **Site settings** are in `docs/hugo.yaml`: the base URL (`https://outofmemory.github.io/webtuidb/`), the navigation bar, dark mode by default with a toggle, wide pages, syntax highlighting (Tokyo Night), copy buttons on code, and `enableGitInfo` (each page shows its last-updated date and an "edit this page" link built from Git history).
- **Custom parts.** `docs/layouts/shortcodes/` (below), `docs/assets/css/custom.css` (screenshot frames and keycaps) and `docs/static/img/` (screenshots).
- **Generated data.** `docs/data/keymap.json`, `menus.json` and `meta.json`, written by `docs/tools/export-meta.sh` (below).

### Build and preview

```sh
make docs         # hugo -s docs --minify --quiet      -> docs/public/
make docs-serve   # hugo server -s docs --disableFastRender
hugo -s docs --quiet   # a plain build: prints every warning and error
```

`docs/public/` and `docs/resources/_gen/` are git-ignored. After editing a page, run `hugo -s docs --quiet`: a broken `relref` link, an unknown shortcode or a front-matter error is reported with the file name. A page that links to another with `{{</* relref "…" */>}}` fails the build if the target does not exist, so links cannot rot silently.

### Shortcodes

| Shortcode | Use |
|---|---|
| `{{</* key "C-d f" */>}}` | A key sequence as keycaps. Write the key in the app's own notation: `C-d` for the prefix, `M-f` for Alt, `S-Tab`, `Enter`, `Esc`, plain `j`. |
| `{{</* action "commit" */>}}` | An action name in code format with its default binding, from `data/keymap.json`. |
| `{{</* ui tui="stem" web="stem" */>}}Caption{{</* /ui */>}}` | A screenshot pair. The stems are file names under `static/img/tui/` and `static/img/web/` without `.png`; either side may be left out. `tui-alt` and `web-alt` override the alt text, `width="half"` puts the two side by side. |
| `{{</* keymap-table */>}}` / `{{</* keymap-table context="grid" */>}}` | The full binding table (or one context), rendered from `data/keymap.json`. The Shortcuts page is this. |
| Hextra's `callout`, `cards`/`card`, `details`, `tabs`/`tab`, `steps`, `filetree` | As described in `docs/WRITING.md`. |

(In the shortcode examples a `/` is written after the opening brace so that Hugo does not run them; drop it when you use them.)

## Writing conventions

`docs/WRITING.md` is the style guide. The short version:

- Every feature page covers **both UIs**: one or two sentences on what it is, a screenshot pair, **how to open it** (terminal keys first, then the web: the same prefix sequence, plus the toolbar button, menu entry or right-click item), **what you can do** (every control, option and operator, in the order they appear), then **details and limits** (what happens on commit, what is logged, engine differences, `// shortcut:` limits, with callouts for data loss and engine caveats), and a Settings sub-section naming the `config.toml` keys.
- Second person, present tense, sentence-case headings, short sentences; describe what happens, not what the code does. Name files, flags and settings exactly. When the two UIs differ, say both in one sentence.
- Refer to actions by their config name in backticks (`commit`, `filter_add_copy`) so a reader can rebind them; show the default prefix `C-d` rather than the word "prefix", except when explaining the concept.
- Pages must use the words a user would type, because search indexes the text (see below).
- Screenshots come from the real app with the demo data. Never mock one up, crop it into something misleading, or describe a state that was not captured.
- Page front matter: `title` (short, sentence case, what the user calls it), `description` (one sentence), `weight`.

## Adding a page

1. Create `docs/content/docs/<section>/<page>.md` with `title`, `description` and `weight`.
2. Write it to the pattern above, with `ui` shortcodes for the states a reader could not picture from the text.
3. Add a `{{</* card link="<page>" title="…" subtitle="…" */>}}` to the section's `_index.md` (the cards grid). A new section needs a folder, an `_index.md` with `title` and `weight`, and a card in `docs/content/docs/_index.md`.
4. Capture the screenshots (below) and put them in place before you finish: every stem a `ui` shortcode names must exist.
5. `hugo -s docs --quiet` until it prints nothing for your files.

## The screenshot tooling

Everything is in `docs/tools/`. It runs the real binary against the Docker test databases plus a demo data set, in an isolated environment, so nothing in your own `~/.config/webtuidb` is read or written.

| File | Purpose |
|---|---|
| `env.sh` | Sets the isolated environment (below). Sourced by every script. |
| `seed.sh` | Loads the demo "shop" data set into the Compose databases. Re-runnable. |
| `seed/` | The demo data: `postgres-shop.sql`, `mysql-shop.sql`, `redis-shop.txt`, `mongo-shop.js`. |
| `serve.sh` | Starts the web UI on `http://127.0.0.1:7797/#token=devtoken` against the docs config. |
| `tui-shot.sh` | Drives the terminal UI inside a private tmux server, sends keys and renders the screen to a PNG. Prints the plain-text screen so you can check what was captured. |
| `web-shot.mjs` | Drives the web UI with Playwright and saves a PNG; `--text` prints the page text. |
| `svg2png.mjs` | Rasterises the SVG that `freeze` writes, with Chromium, so fonts and glyph fallback apply. Used by `tui-shot.sh`. |
| `export-meta.sh` | Writes `docs/data/*.json` from the running docs server's `GET /api/meta`. |
| `env/` | The docs config and state (below). |
| `package.json` | The Playwright dependency of `web-shot.mjs` (`npm install` once in `docs/tools`; `npx playwright install chromium`). |

Prerequisites: Docker databases up (`make up`), a built binary (`make web build`, so the web app is embedded), `tmux` and [`freeze`](https://github.com/charmbracelet/freeze) (`brew install charmbracelet/tap/freeze`) for terminal shots, Node and Playwright for web shots. If Playwright's Chromium is not installed the script falls back to Chrome, Brave or Edge.

```sh
make up                                   # test databases
docs/tools/seed.sh                        # demo data (re-runnable)
make web build                            # ./webtuidb, the binary the shots use
docs/tools/serve.sh &                     # web UI on port 7797
```

### The isolated environment

`env.sh` points `XDG_CONFIG_HOME` at `docs/tools/env/config` and `XDG_DATA_HOME` at `docs/tools/env/data` (or at `$WEBTUIDB_DOCS_DATA` when set, so concurrent callers can use their own copy of the state). It also sets `WEBTUIDB_BIN` (default `./webtuidb` in the repository root), `WEBTUIDB_WEB_PORT` (7797), `WEBTUIDB_WEB_TOKEN` (`devtoken`) and `WEBTUIDB_WEB_URL`.

- `env/config/webtuidb/config.toml` holds the settings for the shots (`prefix = "C-d"`, defaults otherwise).
- `env/config/webtuidb/connections.toml` defines nine connections to the compose databases: "Shop (Postgres)" (blue, `development`), "Shop prod replica" (red, `production`, read-only, safe mode `alert2`), "Shop (MySQL)", "Shop (MariaDB)", "Analytics (ClickHouse)", "Cache (Redis)", "Shop (MongoDB)", "Events (Cassandra)" and "Sessions (DynamoDB local)". These are the names you pass to `-c`.
- `env/data/` (git-ignored) is the history, saved queries, boards and `state.json` of the docs session.

### The demo data

`seed.sh` creates the database `shop` on PostgreSQL (user and password `webtuidb`, port 55432) with `categories`, `products`, `customers`, `orders`, `order_items`, `payments`, `reviews`, `audit_log` and a key-less `no_pk` table, the schema `analytics` with `daily_sales`, the views `paid_orders` and `customer_summary`, the materialized view `top_products`, functions, a trigger, a procedure, the enum `order_status` and the sequence `invoice_seq`. MySQL gets a smaller `shop`, Redis `shop:*` keys and MongoDB a `shop` database with `customers`, `products` and `orders`. MariaDB, ClickHouse, Cassandra and DynamoDB hold only the Compose seed. `seed.sh` talks to the containers by name (`webtuidb-test-postgres-1` and so on), so the Compose project name must stay `webtuidb-test`.

Rule for every capture: never commit a destructive change to the demo databases. Showing pending edits, previews and dialogs is fine as long as you discard or cancel. If a page needs committed changes (an import, a new table, user management), create objects with a name prefixed by the section (`docs_<section>_…`) and drop them afterwards, or use a scratch database.

### Terminal shots: `tui-shot.sh`

```sh
docs/tools/tui-shot.sh -o docs/static/img/tui/browsing-filter-bar.png -c "Shop (Postgres)" -- sleep:2 j j Enter sleep:1 C-d f
```

| Option | Meaning |
|---|---|
| `-o FILE` | Output PNG (required). The plain-text screen is printed to stdout. |
| `-c NAME` | Connection to open at start (passed as the argument to webtuidb); leave out for the connection list. |
| `-a ARGS` | Extra arguments for `webtuidb`, one string, for example `-a "-keys"`. |
| `-s COLSxROWS` | Terminal size, default `150x42`; `110x30` suits a single dialog. |
| `-b SECONDS` | Wait after start before the first key (default 2). |
| `-d SECONDS` | Wait between keys (default 0.4). |
| `-k` | Keep the tmux session alive (the socket name is printed; `command tmux -L <socket> attach`). |
| `-t` | Also write the raw ANSI capture next to the PNG (`FILE.ansi`). |

After `--` come the keys, as `tmux send-keys` names (`C-d`, `M-f`, `S-Tab`, `Enter`, `Escape`, `Tab`, `Up`, `BSpace`, `F5`, plain letters; quote `'?'` and other glyphs the shell would expand) and two tokens: `sleep:N` waits N seconds and `type:TEXT` types text literally. Each run starts a fresh process, so every shot replays its key path from the start: keep paths short and put `sleep:2` after connecting and after anything that loads.

Under the hood: tmux runs the binary at the chosen size with `TERM=xterm-256color`, `COLORTERM=truecolor` and a UTF-8 locale; `capture-pane -e` gives the ANSI screen; `freeze` turns it into an SVG (window frame, JetBrains Mono 13, background `#1a1b26`), and `svg2png.mjs` renders that at 2x.

### Web shots: `web-shot.mjs`

```sh
node docs/tools/web-shot.mjs --out docs/static/img/web/browsing-filter-bar.png \
  --steps '[{"click":"text=Shop (Postgres)"},{"wait":1500},{"click":"text=orders"},{"wait":1200},{"keys":["Control+d","f"]},{"wait":400}]' --text
```

The viewport is 1440x900 at device scale 2, dark colour scheme, reduced motion. Options: `--width`, `--height`, `--light`, `--mobile` (390x844), `--full` (full page), `--clip "x,y,w,h"`, `--base`, `--token`, `--settle MS` (wait before the shot, default 400), `--steps '[…]'` or `--steps-file file.json`, `--script file.mjs` (`export default async (page, {shot}) => {}`) for anything the step list cannot express, and `--text` to print the page's visible text.

Steps are objects with one key: `goto`, `click`, `dblclick`, `rightclick`, `hover` (Playwright selectors: CSS, `text=…`, `role=button[name="Run"]`), `fill` (`["selector","text"]`), `type`, `press` (`Control+d`), `keys` (a list of presses, as for a prefix sequence), `wait` (ms), `waitFor`, `scroll`, `eval`, `shot` (an extra screenshot mid-way) and `viewport`. A failed run writes `<name>.failed.png` next to the target. Page errors seen in the console are printed.

Always read the text dump (`--text`, or the screen the terminal script prints) before keeping a shot: it is how you confirm the screenshot shows the state the caption says.

### Regenerating a screenshot

There is no manifest of key paths. A screenshot's file name is `<section>-<feature>[-<state>].png` (lowercase, hyphenated, the same stem for the terminal and web shot of the same state), and the page that shows it says what state it depicts. To redo one: bring the databases to the demo state (`make up`, `seed.sh`), start `serve.sh` for web shots, rebuild the binary if the UI changed (`make web build`), replay the capture with the same output path, check the text dump, and run `hugo -s docs --quiet`. After a change to bindings or menus, also run `docs/tools/export-meta.sh` and commit the updated `docs/data/*.json`.

### Exporting the data files

```sh
docs/tools/serve.sh &
docs/tools/export-meta.sh
```

The script calls `GET /api/meta` on the docs server and writes `docs/data/keymap.json` (prefix, bindings, and a per-action summary used by the `action` shortcode), `docs/data/menus.json` (the context-menu lists from `internal/menus`) and `docs/data/meta.json` (drivers, safe modes, SSL modes, operators per dialect, password-command presets, export defaults, tags, themes, version). Pages read them with `site.Data`; that is how the shortcuts table always matches the binary.

## Search

Search is FlexSearch, built into the theme (`params.search` in `hugo.yaml`: `type: flexsearch`, `index: content`, `tokenize: forward`). At build time Hextra writes an index of every page's title, headings and text to `docs/public/en.search-data.<hash>.json`; the browser loads it when the search box opens, and matching runs locally. There is nothing to maintain, but there are two consequences for authors: only text that is on the page is searchable (a word that appears only in a screenshot cannot be found), and `forward` tokenising matches the beginning of words, so name features with the words a user would type: the action name, the key, the dialog title.

## Publishing

The `docs` workflow (`.github/workflows/docs.yml`) builds the site with Hugo 0.169.0 on every push to `main` that touches `docs/**` and deploys it to GitHub Pages; it can also be started by hand. The jobs, permissions and the full-history checkout that `enableGitInfo` needs are described on [Testing]({{< relref "testing" >}}). The Pages setting of the repository must use GitHub Actions as its source. To check the exact build the workflow does, run `hugo -s docs --minify --baseURL "https://outofmemory.github.io/webtuidb/"` locally.

To update Hextra, use the standard Hugo module commands from `docs/` (`hugo mod get -u github.com/imfing/hextra`, then `hugo mod vendor`) and rebuild.
