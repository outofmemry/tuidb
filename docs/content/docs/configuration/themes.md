---
title: Themes
description: Built-in colour themes, auto mode, single-colour overrides, theme files, installing themes from git, and how the web maps the palette.
weight: 40
---

A theme is a palette of named colours. Both UIs read the same `[theme]` table in `config.toml`: pick a built-in palette by name, layer a theme file on top, and override single colours. The terminal uses the whole palette; the web maps the subset that has a meaning in the browser.

{{< ui tui="configuration-theme-jade-dark" web="configuration-theme-jade-dark" >}}`[theme] name = "jade-dark"`: the terminal's start screen and the web's Connections page.{{< /ui >}}

{{< ui tui="configuration-theme-sunset-dark" web="configuration-theme-sunset-dark" >}}`[theme] name = "sunset-dark"`.{{< /ui >}}

## Choosing a theme

```toml
[theme]
name = "jade-dark"
```

| Name | What it is |
|---|---|
| `dark` | The default, a Tokyo-Night style dark palette. |
| `light` | Cool slate light palette. |
| `auto` | `dark` or `light`: the terminal UI follows the background colour the terminal reports (and switches when it changes); the web follows the browser's `prefers-color-scheme`. |
| `silver-dark`, `jade-dark`, `polaris-dark`, `sunset-dark` | Dark variants built on `dark`. |
| `sunset-light` | A light variant built on `light`. |
| any installed theme | The folder or file name of a theme in `~/.config/webtuidb/themes/` (see [Installing themes](#installing-themes-from-git)). |

Names are forgiving: `"Silver Dark"` and `"silver_dark"` both mean `silver-dark`. A name that is neither built in nor installed stops the program: `theme.name: "nord" is not one of dark|light|auto|silver-dark|…`.

### How to change it

| | Terminal | Web |
|---|---|---|
| Settings editor | {{< key "C-d ," >}}, `theme.name`, {{< key "Enter" >}} opens the picker, then {{< key "s" >}} | {{< key "C-d ," >}} or **Settings**, section **theme**, the **name** drop-down, **Save** |
| Takes effect | at once, after the save | at once on that page |

The picker lists the built-in names first, then every installed theme. `theme.file` is read-only in the web form (it names a file on the server).

## Layers

The final palette is built in this order; each layer wins over the one before:

1. The named palette (`[theme] name`). An installed theme starts from `dark` here.
2. The `[theme.colors]` table of `[theme] file`, when set. `$VARS` and a leading `~/` in the path are expanded.
3. `[theme.colors]` in `config.toml`.

Five keys follow another one unless you set them yourself, so changing one colour recolours everything that matches it: `tabs_bg` follows `header_bg`, `tab_active_bg` follows `accent`, `notification_bg` follows `status_bg`, `grid_line` follows `border`, `cell_selection` follows `sel_cell`.

## Colour overrides

```toml
[theme]
name = "dark"

[theme.colors]
accent = "#ff9e64"
"tag.production" = "#ff0000"
"driver.postgres" = "33"     # an ANSI colour number 0-255
```

A value is `#rgb`, `#rrggbb` or an ANSI colour number `0`–`255`. The keys:

| Key | Terminal UI | Web |
|---|---|---|
| `accent` | focus, titles, keys in hints, active borders | `--accent` (outside a workspace; inside, the connection's colour is the accent) |
| `dim` | secondary text, NULL (italic), hint labels | `--ink-2` |
| `error` | error messages | `--del` (deleted marks, errors) |
| `ok` | success messages | `--new` (new rows) |
| `warn` | warnings, the unsaved `*` | `--edit` (edited cells) |
| `border` | box borders | `--rule-2` |
| `selection` | the selected row | `--raised` |
| `header_bg` | grid header, the row under the cursor | – |
| `sel_cell` | the selected cell | – |
| `sel_cell_fg` | text on the selected cell and on the active tab | – |
| `modified_row` | edited rows | – |
| `new_row` | added rows | – |
| `deleted_row` | deleted rows (also struck through) | – |
| `alt_row` | shading of every other row | – |
| `keyword`, `string`, `comment`, `number` | SQL syntax colours | `--syn-keyword`, `--syn-string`, `--syn-comment`, `--syn-number` (the SQL editor) |
| `status_bg`, `status_fg` | the status line | – |
| `prefix_bg`, `prefix_fg` | the prefix indicator while a prefix is pending | – |
| `popup_bg` | dialogs and pop-ups | – |
| `editor_bg` | the SQL editor's background | `--editor-bg` |
| `window_bg` | the terminal background (sent to the terminal; `#rrggbb` only) | `--window-bg` (page background) |
| `sidebar_bg` | the sidebar | `--sidebar-bg` |
| `tabs_bg` | the tab bar, idle tabs | `--tabs-bg` |
| `tab_active_bg` | the active tab | `--tab-active-bg` |
| `notification_bg` | status-line notices | `--notification-bg` |
| `grid_line` | lines inside grids | `--grid-line` (only when set) |
| `cell_selection` | the selected cell's background | `--cell-selection` (only when set) |
| `tag.local`, `tag.testing`, `tag.development`, `tag.staging`, `tag.production`, `tag.<your tag>` | tag chips | – |
| `driver.postgres`, `driver.mysql`, `driver.mariadb`, `driver.clickhouse`, `driver.redis`, `driver.mongodb`, `driver.cassandra`, `driver.dynamodb`, `driver.redshift`, `driver.greenplum`, `driver.vertica`, `driver.singlestore` | driver names in the connection list | – |

The last eight surface keys (`editor_bg` to `cell_selection`) are the newest; a theme written before them still works, because they fall back to the palette.

An unknown key or a value that is not a colour does nothing. The terminal UI says so at start in its status line, for example `[theme.colors]: accent = "blue" is not a color (#rrggbb or 0-255); nope is not a theme color`; a theme file that does not parse is reported the same way (`theme file <path>: …`). `tag.*` and `driver.*` accept any name.

{{< callout type="info" >}}
The web uses CSS colours, so an ANSI number such as `"33"` in `[theme.colors]` has no effect in the browser. Use `#rrggbb` for colours both UIs should show.
{{< /callout >}}

## Exporting the palette

```sh
webtuidb -theme > my-theme.toml
```

prints the resolved palette (name, file and overrides applied) as a complete `[theme.colors]` table, sorted by key. Paste it into `config.toml`, or save it as a theme file and point `[theme] file` at it, then edit the colours you want.

## Theme files

A theme file is a TOML file with a `[theme.colors]` table, like the output of `webtuidb -theme`. Point at it with:

```toml
[theme]
name = "dark"                       # the base
file = "~/dotfiles/webtuidb/ocean.toml"
```

The web also accepts the colours as a top-level `[colors]` table in that file; the terminal reads only `[theme.colors]`.

## Installing themes from git

```sh
webtuidb -theme-install https://github.com/someone/webtuidb-ocean.git
webtuidb -theme-install https://github.com/someone/webtuidb-ocean.git@v1.2   # a tag or branch
webtuidb -theme-update                                                      # git pull every installed theme
```

- `-theme-install` runs `git clone --depth 1` (with `--branch <ref>` after `@`) into `~/.config/webtuidb/themes/<name>`. The name is the last part of the URL without `.git`; characters other than letters, digits, `.`, `_` and `-` become `-`. An `@` inside `user@host` is not a ref.
- The repository must have a `theme.toml` at its top level; otherwise the clone is removed: `<url> has no theme.toml at its top level; nothing was installed`.
- Installing a name that exists fails: `theme "ocean" is already installed (<path>); use -theme-update`.
- It prints `installed theme "ocean"; set [theme] name = "ocean"`. Then set the name in `config.toml` or pick it in the settings editor.
- `-theme-update` runs `git pull --ff-only` in every theme folder that is a git checkout, prints each result, and fails with `could not update: <names>` when any pull failed. With none: `no git-installed themes in <dir>`.
- git runs without prompting for credentials and has a time limit.

You can also install a theme by hand: put `<name>/theme.toml` or `<name>.toml` in `~/.config/webtuidb/themes/`. Names starting with `.` are ignored.

An installed theme's colours go on top of `dark`. When you set both `name` (an installed theme) and `file`, `file` wins.

In the web, a theme whose name ends in `-light` (or `light`) switches the page to light mode; every other name uses dark mode, with the theme's colours on top.

## 16-colour terminals

On a terminal without 256-colour or truecolor support, the terminal UI maps every `#rrggbb` colour to the nearest of the 16 ANSI colours, so it stays readable instead of guessing. It decides from `COLORTERM` and `TERM`: any `COLORTERM`, or a `TERM` containing `256`, `direct`, `truecolor`, `kitty`, `ghostty`, `wezterm` or `alacritty` counts as rich. Force it with the environment variable:

| `WEBTUIDB_COLORS` | Effect |
|---|---|
| `16` | always use the 16-colour palette |
| any other value (`truecolor`, `256`) | never use it |
| unset | decide from the terminal |

## How the web maps the palette

The web UI does not copy the terminal palette one to one. Its own design (`web/DESIGN.md`) keeps a few rules:

- **The connection's colour runs the workspace.** Once connected, the connection's `color` is the only accent: the badge, the active tab, the selected row, the primary button, the focus ring. The theme's `accent` shows outside a workspace (the Connections page, the settings dialog). A connection without a colour uses the terminal's blue `#7aa2f7`.
- **Dark or light** comes from the name: `light` and `*-light` themes use light mode, `auto` follows `prefers-color-scheme`, everything else is dark.
- **Named palettes** are a smaller copy in the web: each built-in variant sets `accent`, `error`, `ok`, `warn`, `dim`, `border` and `selection`. The other keys keep the web's own dark or light values unless your file or `[theme.colors]` sets them.
- **Marks never change hue.** Edited cells, deleted rows and new rows follow `warn`, `error` and `ok`, which you can override; the web has no separate keys for `modified_row`, `new_row` or `deleted_row`.
- `[web] font_size` and `[web] line_height` set the page font size and line height.

## Settings

`[theme] name`, `file` and `colors`: [Settings reference → `[theme]`](../settings-reference/#theme).
