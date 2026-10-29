---
title: The browser app
description: The start page, the workspace chrome, the toolbar, keyboard shortcuts and their limits in a browser, right-click menus, drag and drop, themes and accessibility.
weight: 20
---

The web UI is the same engine as the terminal UI with a browser front end, so every feature page in this manual applies to both. This page is about what is specific to the browser: how the start page and the workspace are laid out, how the prefix keys behave in a page, and what only a mouse or a browser can do.

## The start page

The start page lists your saved connections. It is the page the printed address opens, and the page **All connections** returns to.

{{< ui tui="web-ui-start-page" web="web-ui-start-page" >}}
The connection list in the terminal and on the browser's start page: same connections, same groups.
{{< /ui >}}

| Part | What it does |
|---|---|
| **Open file…** | Pick a `.sql` file from your computer and read it without connecting. It opens in a read-only viewer at `/sql` with a tab per file, an **Open file…** button and a **Recent…** list. Open the file from a workspace to run it. The viewer keeps its tabs until you reload the page. {{< key "C-d O" >}} (`open_file`) does the same; {{< key "C-d M-O" >}} (`open_recent`) opens the recent list. |
| **Settings** | The settings editor ({{< key "C-d ," >}}, `settings`). See [Configuration](../../configuration/). |
| **Import…**, **Export…** | Move connections between machines as a `connections.wtdb` file, optionally encrypted with a passphrase. **Export…** is disabled while there are no connections. See [Connections](../../connections/). |
| **Add connection** | The new-connection form. |
| **Paste a connection URL** | Type or paste a URL such as `postgresql://user:pw@host/db?name=Prod&env=production` and press {{< key "Enter" >}} (or **Continue**). The server parses it and the new-connection form opens filled in. A URL that does not start with a database scheme says so in plain words. |
| Engine buttons | One button per engine, with its mark. Click one to open the new-connection form with that driver selected. |
| **Sort by** | **Name**, **Tag**, **Driver** or **Recent** (last connected first). It shows only when there is more than one connection. The choice is saved on the server, so the terminal UI uses it too; `[general] connection_sort` sets the default. |
| Group headers | Connections with the same `group` are listed under a header with a count. **Rename** renames the group; **Delete** asks you to type the group's name and then deletes it with every connection in it. Drag a connection onto a header to move it there, or onto the strip that appears above the list (**Drop here to take it out of its group**) to remove its group. |
| A connection row | A colour bar, the engine mark, the name, the tag, a **read only** chip when the connection refuses writes, a **source** chip for connections that come from a `[[source]]` file (edit that file to change them), **● open** when a workspace for it is already open in this browser tab, the engine and address, and **connected 4 minutes ago**. |
| **Connect** or **Resume** | Opens a session and the workspace. A connection that is already open in this browser tab shows **Resume** and returns to it. The row itself is a button: click anywhere on it, or focus it with {{< key "Tab" >}} and press {{< key "Enter" >}}. |
| **⋯** or right-click | The row menu: **Connect**, **Edit…**, **Duplicate**, **Delete…**, **Test**, **Copy URL**, **Copy URL with password**, **Import URL…**, **Export connections…**, **Import connections…**, **Sort by…**. The keyboard menu key or {{< key "S-F10" >}} opens it on a focused row. |

{{< ui web="web-ui-start-row-menu" >}}
The row menu, opened with a right-click. It is the same list as the terminal's connection menu.
{{< /ui >}}

{{< ui web="web-ui-start-sort" >}}
**Sort by** on the start page.
{{< /ui >}}

{{< ui web="web-ui-start-url" >}}
After pasting a URL the form opens filled in; nothing is saved until **Save**.
{{< /ui >}}

Keyboard: on the start page the prefix keys that make sense there work: {{< key "C-d ?" >}} (key bindings), {{< key "C-d ," >}} (settings), {{< key "C-d O" >}} and {{< key "C-d M-O" >}} (open a file, recent files).

If a connection cannot open, a panel under the URL box says what to check in one sentence (for example "Nothing answered at that address. Check that the database server is running, and that the host and port are right."), with **Edit connection…**, **Try again** and a **Technical details** fold that holds the driver's own text. A connection whose password is set to ask opens a password prompt first.

With `[general] reopen_workspaces` (the default), the first time a browser tab visits the start page it reopens the workspaces that were open the last time, each on the database it was showing, and lands on the last one. A workspace that fails to reopen is reported in the same panel.

## The workspace

Connect to a connection and the workspace opens at `/s/<session id>`.

{{< ui tui="web-ui-workspace" web="web-ui-workspace" >}}
A workspace on `orders`: top bar, sidebar, tabs, grid, status bar.
{{< /ui >}}

From the top down: the **top bar** (connection, safe mode, database, toolbar), the **tab bar**, the **content** with the sidebar on the left, the optional **console pane**, and the **status bar**.

### The top bar

| Part | What it does |
|---|---|
| Connection badge | Painted in the connection's colour. Shows the engine mark and the connection name; click it for the switcher. |
| Server text | The server's version string and the connection encoding. Hidden on a phone. |
| Safe mode chip | The current level (**Silent Mode**, **Alert Mode 1** and so on). Click it to change the level ({{< key "C-d S" >}}, `safe_mode`). See [Safety](../../safety/). |
| **read only** chip | Shown when the connection refuses every statement that changes data. |
| Database picker | The current database with a caret, when the server has more than one. Click it ({{< key "C-d k" >}}, `open_database`) for the list; right-click a name or use its **⋯** for **Open in current tab**, **Open in new workspace**, **Copy database name**, **New database…**, **Rename…**, **Drop…**. Switching closes the tabs of the workspace after asking. |
| Toolbar | The buttons on the right, see below. |

**The connection switcher** lists every saved connection with its colour bar and engine mark. Connections that are open in this browser tab show the database they are on. Choosing one resumes its workspace, or opens it; this workspace stays open on the server, so coming back is instant. The bottom of the list has **Close other connections**, **MCP activity** and **All connections** (the start page). `[general] show_connection_name` and `show_database_name` choose which of the two an open workspace's entry shows (both when neither is set).

{{< ui web="web-ui-workspace-switcher" >}}
The connection switcher.
{{< /ui >}}

### The toolbar

`[web] toolbar` lists the toolbar's buttons by action name, in order. Empty (the default) means:

```toml
[web]
toolbar = ["new_query_tab", "history", "saved_queries", "open_anything", "help", "disconnect"]
```

| Name | Button | Notes |
|---|---|---|
| `new_query_tab` | **New query** | |
| `history` | **History** | Toggles the history drawer; shown pressed while it is open. |
| `saved_queries` | **Saved queries** | Toggles the saved-queries drawer. |
| `open_anything` | **Open anything** | The palette: tables, saved queries, databases and actions. |
| `help` | **?** | Opens the key bindings. |
| `disconnect` | **Disconnect** | Closes this workspace's session. |
| `jobs` | **Jobs** | Toggles the background jobs drawer. |
| any other action name | its description | Any action the web UI implements, for example `toggle_console`, `process_list`, `search_in_database`, `diagram`, `backup`. It runs the action; the tooltip shows the key. `plugin:<name>` runs a plugin. |

Names are checked when the config loads: an empty name, a name listed twice, or a name that is not an action is an error that names the line. A name that is valid but not implemented in the browser is skipped. The hover text of each button ends with its key, taken from your resolved keymap.

{{< ui web="web-ui-toolbar-custom" >}}
`toolbar = ["new_query_tab", "open_anything", "jobs", "toggle_console", "help", "disconnect"]`: `toggle_console` has no button name of its own, so it shows its description, **toggle console log**.
{{< /ui >}}

Below 980 px wide every button except **New query**, **Open anything** and **?** moves into a **⋯** menu; below 760 px all of them do. That menu always lists **New query**, **History**, **Saved queries**, **Open anything**, **Key bindings** and **Disconnect**, whatever the toolbar setting says. The terminal has no toolbar; it shows hint chips in its status line (`[general] hints`).

### Tabs and panes

Tabs hold tables, query editors and the three pages (**Diagram**, **Metrics**, **Assistant**, one each). A table tab shows the schema in front of the name when it is not the current schema; an editor tab is **Query 1**, **Query 2** and so on, until you rename it or save it.

- Click a tab to show it; the **+** button or {{< key "C-d e" >}} opens a query editor; double-clicking the empty part of the tab bar runs `new_tab` (a query, or the sidebar search with `[general] new_tab_opens = "empty"`).
- The tab menu (right-click, the **▾** on the tab, or {{< key "S-F10" >}} on a focused tab) lists **Close**, **Close other tabs**, **Close tabs to the right**, **Close all tabs**, **Rename tab…**, **Move left**, **Move right** and two web-only entries: **Open workspace in a new browser tab** and **Copy link** (see [Deep links](../deep-links/)). {{< key "Alt+Shift+←" >}} and {{< key "Alt+Shift+→" >}} move a focused tab.
- A tab with unsent changes shows an amber ● in place of its ✕. Closing it asks first (**Discard pending changes in the tab(s) being closed?**); so does closing a query tab that holds an open transaction (**This tab has an open transaction. Close it and roll back?**). Leaving or reloading the page with unsent changes or an open transaction triggers the browser's own "leave site?" prompt.
- {{< key "C-d \"" >}} (`split_right`) moves the current tab into a second pane on the right; on a phone the two panes stack. {{< key "C-d M-q" >}} (`close_pane`) puts the tabs back together. Inside a query tab {{< key "C-d M-\"" >}} (`split_down`) stacks a second editor.
- Reloading the page returns to the same tabs, including unsent editor text (kept per browser tab; a tab whose first line is `-- sensitive` is never stored, and a tab's text is cut at 64 KB). `[editor] auto_save_tabs` (`none`, `first` or `all`; the default is `first`) also saves the editors on the server, so a fresh session, or the terminal, starts with the first editor (or all of them) back.

{{< ui web="web-ui-tab-menu" >}}
The tab menu.
{{< /ui >}}

### The sidebar

The object list on the left is the [sidebar](../../browsing-data/). Below 760 px wide it becomes a drawer: an **Objects** button appears in the badge, the list slides in over the page with a dimmed backdrop, and choosing an object or tapping the backdrop closes it. {{< key "C-d 0" >}} (`toggle_sidebar`) shows or hides it on a wide window too. The sidebar is also where you drop a data file onto a table, see [Drag and drop](#drag-and-drop).

### The changeset tray

Edits in a grid are staged, not sent. The first staged edit raises the tray at the bottom of the table tab:

{{< ui tui="web-ui-changeset-tray" web="web-ui-changeset-tray" >}}
Staged changes: an edited cell, a deleted row. Nothing is sent until **Send changes**.
{{< /ui >}}

The tray counts the staged changes (`2 staged changes Nothing is sent until you choose Send changes.`) and lists each one: an edited cell as `status on id 1: paid shipped` with the old value struck through, a deleted row as `remove id 3` struck through, a new row with a green `+`. Each line has **Undo**. While a deleted row is selected, the table's **Delete row** button reads **Keep row** and takes the delete back. The terminal has no tray: it shows `● 2 pending` above the key hints, `● unsaved` in its header and a ● on the tab, and {{< key "C-d P" >}} lists the statements. **Hide list** and **Show list** fold the list; **Preview SQL** ({{< key "C-d P" >}}) shows the statements; **Discard all** ({{< key "C-d Backspace" >}}) drops them; **Send changes** ({{< key "C-d s" >}}) opens the confirmation that safe mode asks for and then commits. See [Editing data](../../editing-data/).

### The console pane

{{< key "C-d C-c" >}} (`toggle_console`) opens a 200 px pane above the status bar with the statements this server sent to the database for this connection, with timestamps. It refreshes every two seconds, keeps the last 1000 lines and has **Find**, **Errors only**, **Clear** and **✕**. Its own keys, when it has the focus: `/` find, `n` and `N` next and previous line, `y` copy the line, `e` errors only, `m` the line menu. Right-click a line for **Find…**, **Copy line**, **Errors only**, **Clear**.

{{< ui tui="web-ui-console" web="web-ui-console" >}}
The console log: every statement sent to the database, newest last.
{{< /ui >}}

### The status bar

A 24 px bar along the bottom: a green dot and the connection name, **details**, the database and schema, and on the right **Jobs** with a count of running jobs (or of all jobs when none runs). Click the name to **reconnect**: the page asks first, because unsent changes are lost; then it opens a fresh session for the same connection, carries the open tabs over, and closes the old session. **details** opens the connection details dialog. **Jobs** toggles the drawer of background imports, exports, backups, restores and searches; a finished export has a **Download** entry (the terminal has **Open folder** instead).

{{< ui web="web-ui-reconnect" >}}
Clicking the connection name in the status bar.
{{< /ui >}}

{{< key "C-d M-0" >}} (`toggle_panels`) is zen mode: it hides the sidebar, the console, the row detail and the status bar, and a toast says which key brings them back.

## Keyboard shortcuts in a browser

The web UI runs the same tmux-style state machine as the terminal (`web/src/utils/keys.ts`, mirroring `internal/keymap`). The bindings and the prefix come from `GET /api/meta`, so your `[general] prefix` and `[keys]` overrides apply to both front ends. Press the prefix (default {{< key "C-d" >}}), then a key. The full list is in [Shortcuts](../../reference/shortcuts/).

How the state machine treats keys:

1. **Prefix, then a key.** The first press arms the prefix. A panel appears at the bottom right with the keys that are valid in the focused pane, sorted by key. The terminal's panel lists the focused pane's keys and then the global ones sorted by description, ends with `… N more: C-d ? lists them all` when they do not fit, and shows `PREFIX` in the mode box of its status line.
2. **The focused pane decides.** Each pane (sidebar, grid, editor, results, structure, filter bar, console) is a region marked with a context. The keys of the focused context are searched first, then the global ones. The focus is the element that has it, or the last thing you clicked: grid cells cannot take focus, so a click on a cell is what puts you in the grid.
3. **{{< key "Esc" >}}** cancels the armed prefix; a click anywhere cancels it; a key that is bound to nothing closes the panel and does nothing.
4. **The prefix twice** sends the prefix itself to the page, like tmux. In a browser that is the key's own meaning: {{< key "C-d C-d" >}} bookmarks the page in most browsers.
5. **Typing is never intercepted.** In a text field (an input, a textarea, the editor) a plain printable key is text, not a shortcut. The prefix itself still arms in a text field, so {{< key "C-d s" >}} commits while you are typing in the filter bar. Keys that need no prefix (the pane focus keys {{< key "C-h" >}}, {{< key "C-j" >}}, {{< key "C-k" >}}, {{< key "C-l" >}}, the filter bar's {{< key "M-n" >}}, {{< key "M-c" >}}, {{< key "M-e" >}} and the console's keys) keep their no-prefix meaning.
6. **Tab-level actions** are answered by the page that is showing. If nothing claims an action, a toast says `<description>: nothing to do here`.
7. A locked page (see `lock`) ignores every shortcut.

Every action the web UI implements is in its `SUPPORTED` list, and every action in the default keymap is on it, so no binding is greyed out today. The help dialog still shows a binding the web UI does not implement in a dimmer colour, titled "Not in the web UI", and a plugin shortcut (`plugin:<name>`) always counts as supported; if a future action is terminal-only, that is how you will see it. Pressing it shows `Not in the web UI: <description>`.

{{< ui tui="web-ui-help" web="web-ui-help" >}}
The help dialog ({{< key "C-d ?" >}}) in a grid: the grid's keys first, then the global ones, in the same order in both UIs. The terminal's header line reads "Every action is C-d then a key · C-d C-d sends the raw key · Esc cancels the prefix".
{{< /ui >}}

The help dialog lists the focused pane's bindings and then the global ones, in keymap order, exactly as the terminal's help does. The search box in the browser matches, as a case-insensitive substring, the description, the key and the action name (`commit`, `M-Enter`, `filter`); the terminal's help search is fuzzy, so a scrambled `fltr` finds filters there but not here. {{< key "Esc" >}} in the box clears the search first, then closes the dialog.

{{< ui tui="web-ui-prefix-panel" web="web-ui-prefix-panel" >}}
After {{< key "C-d" >}} the panel lists what each key does here.
{{< /ui >}}

### Keys a browser keeps for itself

The page sees almost every key. A few chords never reach it, and a few it takes over:

| Key | Terminal | In the browser |
|---|---|---|
| {{< key "C-d C-d" >}} | sends a literal {{< key "C-d" >}} to the focused widget | passes through to the page, where most browsers use {{< key "C-d" >}} to bookmark. In the SQL editor, plain typing needs no prefix, so you rarely want it. |
| {{< key "C-h" >}}, {{< key "C-j" >}}, {{< key "C-k" >}}, {{< key "C-l" >}} | move between panes | the page takes these over while it is open, so they no longer open history, downloads, the search box or the address bar. Rebind them with `[keys]` (for example `focus_right = "-n M-l"`) if you need the browser's, or use the prefixed `C-d M-h/j/k/l`. |
| {{< key "Cmd+…" >}} on a Mac | not used | never intercepted: Cmd shortcuts keep their browser meaning. The prefix is the Control key, not Command. |
| {{< key "C-w" >}}, {{< key "C-n" >}}, {{< key "C-t" >}} | not bound | the browser handles these before a page does, which is why no default binding uses them (a test keeps `close_all_tabs` off {{< key "C-w" >}}). |
| {{< key "M-…" >}} on a Mac | Option as Meta, if your terminal is set up for it | matched on the physical key, so {{< key "M-f" >}} works with the Option key even though the browser would type a special character. |
| Operating system chords | | some chords are taken by the system or the browser (for example Alt+letter menu accelerators on Windows and Linux). If a chord does not arrive, run the action from the toolbar, from **Open anything** ({{< key "C-d p" >}}, which lists every action by name) or rebind it. |

A shortcut that cannot work in a browser is never skipped silently: it stays in the shared keymap and either has a web alternative as above, or the **Open anything** palette runs it by name.

## Right-click menus

Right-clicking opens a menu in the same place the terminal's `m` or Menu key does, with the same entries in the same order (the lists come from `GET /api/meta`, shared with the terminal). Menus exist on connection rows, tabs, databases, sidebar objects, saved queries and history entries, grid cells, column headers, result tabs, console lines, jobs and the process list, the filter bar, and the structure editor. Entries the browser cannot do (for example **Add to metrics board…** from a result) are shown greyed out with the reason as the tooltip; entries only the browser has (**Copy link**, **Download**) show only here.

{{< ui web="web-ui-grid-menu" >}}
The grid cell menu.
{{< /ui >}}

Every floating menu (this one, the connection switcher, **Select** drop-downs, the **⋯** menus) is drawn by one component, `Layer` (`web/src/components/shell/Layer.tsx`), and so behaves the same:

- It renders in a layer above every dialog and sticky bar, with a backdrop that swallows the click that closes it, so nothing underneath is pressed by accident.
- It closes on an outside click, a right-click outside, {{< key "Esc" >}}, when the window loses focus and when the window is resized. An open menu takes {{< key "Esc" >}} before a dialog under it.
- Arrow keys move through the entries, {{< key "Home" >}} and {{< key "End" >}} jump, {{< key "Tab" >}} closes it, and in a drop-down typing a letter jumps to the next entry that starts with it.
- It opens from the keyboard too: the menu key, or {{< key "S-F10" >}}, on a focused row, tab, saved query or history entry.

## Drag and drop

| Drag | Drop on | Result |
|---|---|---|
| A `.csv`, `.tsv`, `.json` or `.sql` file | a table in the sidebar, or a table tab | Opens the import wizard on that table with the file loaded. |
| A `.sql` file | anywhere in a workspace except a table (an editor, a view, a page) | Opens it in a new query editor tab. |
| Any other file | a table tab or sidebar table | A toast says what you can drop. |
| A connection row | a group header, or the strip above the list | Moves it to that group (not for connections from a source file). |
| A tab | another tab, or the other pane | Reorders: dropping on the left half puts it before, the right half after. Across the split it changes pane. |
| A saved query | a folder header, or the strip above the list | Moves the file to that folder. Shared queries cannot move. |
| A column's handle in the structure editor | another column | Reorders the columns on engines that support it. |
| A panel on a metrics board | another panel | Rearranges the board (kept in this browser). |

## Looks

**Theme.** `[theme] name` picks the palette: `dark` (the default) and `light` are fixed; `auto` follows the browser's `prefers-color-scheme`; the named themes `silver-dark`, `jade-dark`, `polaris-dark`, `sunset-dark` and `sunset-light` pick dark or light by their suffix and apply a small palette of their own (accent, error, ok, warn, dim, border, selection). An installed theme (`webtuidb -theme-install`) is accepted as a name, but the browser takes only dark from it; point `[theme] file` at its `theme.toml` to get its colours. `[theme] file` and `[theme.colors]` override individual colours: `accent`, `error`, `ok`, `warn`, `dim`, `border`, `selection`, `keyword`, `string`, `number`, `comment`, and the surfaces `editor_bg`, `window_bg`, `sidebar_bg`, `tabs_bg`, `tab_active_bg`, `notification_bg`, `grid_line`, `cell_selection`.

{{< ui web="web-ui-light" >}}
`[theme] name = "light"`.
{{< /ui >}}

**The connection's colour is the accent.** Once connected, the connection's own colour is the only accent in the workspace: the name badge, the active tab underline, the selected row, the primary button, the primary-key marker and the focus ring. A red connection makes the whole workspace red. Without a colour, the tag's colour is used, and without either the terminal's blue `#7aa2f7`. Form fields keep an ivory focus ring, so a red accent is never mistaken for a validation error. Edited cells are always amber, deleted rows coral and new rows green, whatever the accent.

**The production cue.** A connection tagged `production`, `prod`, `live`, `staging` or `stage` (any tag that starts with one of these) gets a 3 px bar of the accent colour across the very top of the workspace, labelled `<tag> database` for screen readers, with the tooltip `<tag>: this is not a scratch database`. The terminal shows the tag as a coloured badge (`PRODUCTION`) and a `READ-ONLY` badge in its header instead.

{{< ui tui="web-ui-production" web="web-ui-production" >}}
`Shop prod replica`: tagged `production`, read only, with the red accent and bar.
{{< /ui >}}

**Fonts and scrollbars.** The workspace uses Martian Mono (narrow width) for chrome, data, the editor and connection strings, and Bricolage Grotesque for prose. `[web] font_size` and `[web] line_height` scale the page. `[web] auto_hide_scrollbars` (default on) hides scrollbars until you scroll or hover a pane.

## Keyboard, screen readers and reduced motion

- Every control is reachable with {{< key "Tab" >}} and has a visible focus ring. Dialogs trap {{< key "Tab" >}} inside, move the focus in when they open and return it to where it was when they close; {{< key "Esc" >}} closes the top dialog only.
- Rows, tabs, menus and dialogs carry roles and labels (`tablist`, `tab` with `aria-selected`, `menu` and `menuitem`, `status` and `alert` for toasts and errors, `region` for the changeset tray and the console), and toggles report `aria-pressed` or `aria-expanded`.
- With `prefers-reduced-motion: reduce` animations are cut to nothing (the blinking cursor on the headings, the rising toast, the loading bar, the drawer slide).
- A failure says what to check in a sentence, and keeps the driver's text behind **Technical details**. Anything that would lose work asks first.

## Settings

| Key | Default | Meaning |
|---|---|---|
| `[web] toolbar` | `[]` | Toolbar buttons, in order. |
| `[web] font_size`, `[web] line_height` | `0`, empty | Page font size and line height. |
| `[web] auto_hide_scrollbars` | `true` | Show scrollbars only while scrolling. |
| `[web] notifications` | `false` | Desktop notification when a backup or import ends. |
| `[theme] name`, `file`, `colors` | `dark` | Palette and overrides. |
| `[general] connection_sort` | `name` | Start page order: `name`, `tag`, `driver`, `recent`. |
| `[general] reopen_workspaces` | `true` | Reopen last time's workspaces on the first visit of a browser tab. |
| `[general] show_connection_name`, `show_database_name` | off | Parts of an open workspace's label in the switcher. |
| `[general] new_tab_opens` | `query` | What `new_tab` does: `query` or `empty`. |
| `[security] passcode`, `lock_after_mins` | unset, `0` | The lock screen; see [Safety](../../safety/). |

## See also

- [Serving the web UI](../serving/), [Deep links](../deep-links/), [On a phone](../mobile/).
- [Shortcuts](../../reference/shortcuts/) for every binding.
