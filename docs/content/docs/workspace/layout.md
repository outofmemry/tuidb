---
title: Layout
description: The frame around your data in the terminal and in the browser, the top bar, the status line, zen mode and the mouse.
weight: 10
---

A workspace is one open connection. Both UIs draw it the same way: a bar across the top that says where
you are, the object sidebar on the left, the tabs and the focused pane in the middle, optional panes for
row details and the console, and a status line at the bottom.

{{< ui tui="workspace-layout" web="workspace-layout" >}}A workspace on the demo database: the sidebar with Recent and Tables, two table tabs (`customers`, `orders`) and the console open below.{{< /ui >}}

## The terminal frame

Every pane is a rounded box. The box that has the keyboard is drawn in the accent colour and is the only one
that shows key hints on its bottom edge, so one set of hints is on screen at a time.

| Part | What it shows |
|---|---|
| Top bar (first line) | The `◆ webtuidb` badge, then the path: a status dot, the connection name, its tag badge (the tag in capitals, `DEVELOPMENT`, or `●` when the connection sets only a colour), `READ-ONLY` when the connection is read only, the driver and `database.schema`, and the title of the active tab. On the right: the safe-mode state, `enc <encoding>` when the connection sets one, `● unsaved` when a tab has pending edits, `sending…` while a commit runs, and `workspace 2/3` when more than one workspace is open. On the connection list the bar shows `N connections` instead of a path. |
| Sidebar | The object tree with three tabs set into its top edge: `1 Objects`, `2 Queries`, `3 History`. When the strip is too narrow the inactive tabs shrink to their digit. See [Sidebar](../sidebar/). |
| Main pane | The tab bar on its top edge (`1 ≡ SQL 1   2 ▦ customers   3 ▦ orders`), the active tab inside, and that tab's key hints on the bottom edge. A tab that is loading adds a spinner and `loading…` after the tab bar. See [Tabs](../tabs/). |
| Row details | Optional pane on the right ({{< key "C-d Space" >}}). See [Panes](../panes/). |
| Console | Optional pane across the bottom ({{< key "C-d C-c" >}}). See [Console](../console/). |
| Status line (last line) | On the left, a pill naming the pane that has the keys (`OBJECTS`, `SAVED QUERIES`, `HISTORY`, `DATA`, `EDITOR`, `RESULTS`, `STRUCTURE`, `FILTER`, `CONSOLE`, `ROW DETAILS`), or `PREFIX` while the prefix is pressed, then where you are: `Ln 3, Col 12` in an editor, the sort columns (`total ▼`) and `2 filter(s)` on a data grid. On the right, key hints, or the latest message (`✓` in green, `✗` in red). |

### The status dot and the safe-mode badge

The dot before the connection name is green while the connection is fine, turns into a yellow spinner while
a query, a load or a commit is running, and turns red when the keep-alive ping fails. A failed ping also
says `lost connection to <name> (...): press C-d R to reconnect`; the next good ping says
`<name> is reachable again`.

`reconnect` ({{< key "C-d R" >}}) asks **Reconnect Shop (Postgres)?** (*The connection is reopened; tabs stay,
unsent edits are kept.*) and reopens the connection on the database that is open now. The web does the same from
the status bar, see below; the details are in
[Sessions and databases](../../connections/sessions-and-databases/).

The safe-mode badge reads `⚠ <level>` in the warning colour for every level that asks before a write. At the
`silent` level it reads `safe mode off`: dimmed normally, in the warning colour on a connection tagged
`production`. Change the level with {{< key "C-d S" >}} (`safe_mode`); see [Safety](../../safety/).

### Production and staging colours

A connection tagged `production` or `staging` tints every pane border with the tag's colour (or the
connection's own colour when it sets one): the focused pane in the full colour, the others faded toward the
normal border colour. The warning is on the whole screen, not only in the top bar.

{{< ui tui="workspace-production" web="workspace-production" >}}The "Shop prod replica" connection: tagged production, read only, safe mode `alert2`. The terminal tints every border; the web paints the accent and draws a thin bar across the top.{{< /ui >}}

### Status-line hints

Out of the box the right side of the status line shows three or four keys for the focused pane and always ends
with {{< key "C-d ?" >}} help. Pairs that the focused pane already shows on its border are left out, and
when that leaves only help, `C-d p find anything` and `C-d e sql` fill the space. When the line is too narrow,
pairs are dropped from the end (help stays).

| Focused pane | Built-in hints |
|---|---|
| Data grid | `↵ edit`, `C-d s commit`, `C-d f filter`, `C-d e sql` (a view: `↵ quick look`, `C-d f filter`, `C-d e sql`) |
| SQL editor | `C-d f find`, `C-d / comment`, `C-d C-Space complete`, `C-d . cancel` (the assistant: `↵ send`) |
| Results | `C-d c copy`, `C-d f search`, `C-d g chart`, `Esc editor` |
| Sidebar | `↵ open`, `/ find`, `m menu`, `C-d e sql` |
| Filter bar | `↵ apply`, `Tab field`, `Esc hide` |
| Structure | `↵ edit`, `C-d i add`, `C-d s commit` |
| Connection list | `↵ connect`, `n new` |

`[general] hints` replaces this set with your own list of action names, shown in that order in every pane.
Each entry shows the action's key in the focused pane and the start of its description (the text before `(`
or `,`). An action with no key in the focused pane is skipped, the help pair is not added, and pairs are
dropped from the end when the line is too narrow.

```toml
[general]
hints = ["commit", "preview", "discard", "open_anything"]
```

The web UI has no status-line hints; `[web] toolbar` plays that role there.

### The prefix panel

Press the prefix ({{< key "C-d" >}}) and a panel slides up over the bottom of the screen until you press the
next key: the bindings of the focused pane first, then the global ones, each group sorted by description and
laid out in columns. `tab_1` to `tab_9` collapse to one `1-9 go to tab` entry. When a group does not fit, its
last line says `… N more: C-d ? lists them all`. {{< key "Esc" >}} cancels; pressing the prefix twice sends
{{< key "C-d" >}} itself to the pane. A key that has no binding after the prefix says
`no binding for that key after the prefix (C-d ? lists them)`.

{{< ui tui="workspace-prefix" web="workspace-prefix" >}}The prefix pressed on a data grid: grid keys first, then the global ones. The web greys the few bindings it does not implement.{{< /ui >}}

{{< callout type="info" >}}
{{< key "C-c" >}} on its own does not quit (it is passed to the pane, outside the editor the status line says
`Quit with C-d q`). Quit is {{< key "C-d q" >}}; it asks first when a commit is running, a `.sql` file
has unsaved changes, a tab holds an open transaction, or a tab has pending edits.
{{< /callout >}}

### The empty workspace

A workspace with no tabs shows the connection, the server version, the database and how many objects the
sidebar loaded (or `Could not load objects`), then a short "Get started" list: {{< key "Enter" >}} to open the
highlighted table, {{< key "/" >}} to find one, {{< key "C-d e" >}} for a SQL editor, {{< key "C-d p" >}}
for Open Anything and the focus keys. A "Then" list follows with editing a cell, preview, commit, discard,
filters, structure and safe mode, and the last lines remind you that {{< key "C-d ?" >}} lists every key and
{{< key "Esc" >}} cancels the prefix. The keys shown are the ones bound in your keymap.

The web shows *Pick a table from the list, or start a new query.* with **New query**, **Open anything** and
**Key bindings** buttons, and a line saying that shortcuts start with the prefix.

{{< ui tui="workspace-empty" web="workspace-empty" >}}A workspace after Close all tabs.{{< /ui >}}

### Size

The terminal needs at least 20 × 6 cells; smaller, it says `webtuidb needs at least 20×6 cells (this
terminal is W×H)`. The sidebar takes a quarter of the width (24 to 40 cells), the row-detail pane a quarter
(30 to 46 cells) and the console a quarter of the height (5 to 12 lines). The console is only drawn when the
body is at least 14 lines tall; when the main pane would be narrower than 20 cells the side panes are not drawn.

## The web frame

| Part | What it shows |
|---|---|
| Connection switcher | The coloured block at the top left: the driver logo and the connection name. Its colour is the connection's `color`, else its tag's colour. Click it for every saved connection (each with its colour bar; open ones show their database), **Close other connections**, **MCP activity** and **All connections** (back to the connection list). Picking a connection that is already open switches to its workspace; the session stays open on the server, so switching back is instant. |
| Top bar | The server version and the connection's encoding, the safe-mode chip (click it to change the level), a `read only` chip, and the database switcher (`shop ▾`, only when the server has more than one database). On the right, the toolbar. |
| Toolbar | `[web] toolbar` lists its buttons by action name, in order. The default is `new_query_tab`, `history`, `saved_queries`, `open_anything`, `help`, `disconnect`. Besides those, `jobs` is a known name, and any other action the web supports becomes a button labelled with its description; a name the web does not support is left out without a message. Below 980 px wide only **New query**, **Open anything** and **?** stay, and a `⋯` button appears; below 760 px those go too. The `⋯` menu always holds the same six entries (New query, History, Saved queries, Open anything, Key bindings, Disconnect), whatever `[web] toolbar` says. |
| Sidebar | Filter box, **+** (create), **↻** (refresh), the schema picker, then Pinned, Recent, your groups and the objects by kind. See [Sidebar](../sidebar/). |
| Tabs | One tab bar per pane, with a **▾** tab menu and **✕** on each tab and **+** for a new query at the end. See [Tabs](../tabs/). |
| Tab toolbar | Each table tab has its own toolbar (Reload, Filters, Columns, the Data / Structure / Index / DDL view menu, Find, Export, `⋯`, Add row, Duplicate, Delete row) and a footer with paging. See [Browsing data](../../browsing-data/). |
| Staged changes tray | When a table tab has pending edits, a tray opens above its footer: `N staged changes`, **Hide list / Show list**, **Preview SQL**, **Discard all**, **Send changes**, and one line per change with its own **Undo**. See [Editing data](../../editing-data/). |
| Side drawers | History, Saved queries and Jobs open as drawers on the right. |
| Console | A pane across the bottom ({{< key "C-d C-c" >}}). See [Console](../console/). |
| Status bar | `● <connection>` (click to reconnect: it asks `Reconnect to <name>? Unsent changes in open tabs are lost.`, opens a fresh session, carries the open tabs over and closes the old one), **details** (connection details), `database.schema`, and on the right **Jobs** with a count: the running jobs in the accent colour, else all listed jobs. |

Messages appear as a toast at the bottom centre for 3.5 seconds. A message longer than 140 characters, or one
with several lines, keeps only its first line in the toast and stays until dismissed, with a **Details**
button that shows the whole text (and **Copy**).

A connection whose tag starts with `prod`, `production`, `live`, `staging` or `stage` also gets a 3 px bar
in the accent colour across the top of the page. (The terminal tints borders only for the exact tags
`production` and `staging`.)

The prefix works in the browser as it does in the terminal: press {{< key "C-d" >}} and a panel at the bottom
right lists the keys for the focused area (`C-d then`). A click anywhere cancels a pressed prefix. Bindings the
web does not implement are listed in grey.

On a phone-sized window the sidebar becomes a drawer (the **Objects** button in the top bar opens it), and the
drawers and the jobs centre cover the screen.

## Zen mode

`toggle_panels` ({{< key "C-d M-0" >}}) hides everything but the tabs; press it again to bring back what was
shown before.

| | Terminal | Web |
|---|---|---|
| Hidden | sidebar, console, row-detail pane, status line | sidebar, console, status bar |
| Kept | tab bar, the tab, top bar | tab bars, the tab, top bar, row details |
| Messages | take the right side of the top bar | toast, as usual (`Panels hidden: C-d M-0 brings them back`, then `Panels shown`) |

The individual toggles still work while in zen: {{< key "C-d 0" >}} (`toggle_sidebar`),
{{< key "C-d C-c" >}} (`toggle_console`) and {{< key "C-d Space" >}} (`toggle_row_detail`). In the terminal,
leaving zen restores the panes as they were when you entered it.

{{< ui tui="workspace-zen" web="workspace-zen" >}}Zen mode on the `orders` table: only the tab bar and the grid are left.{{< /ui >}}

## Mouse in the terminal

The terminal UI turns on mouse reporting, so the wheel and clicks reach it. Hold {{< key "Shift" >}} while
dragging to select text with your terminal as usual.

| Action | What happens |
|---|---|
| Wheel | Three presses of ↑ or ↓ in the focused pane or dialog (a horizontal wheel moves one column). It scrolls what has the keys, not what is under the pointer. |
| Click in the sidebar | Selects the row and focuses the sidebar; a click on the row that is already selected opens it. |
| Click on a tab in the tab bar | Shows that tab. |
| Click in a data grid | Moves the cursor to the cell. |
| Click a column header | Sorts by that column, ascending; a second click sorts descending and a third removes the sort. |
| Drag a header edge | Resizes the column; the width is saved for that table. |
| Double-click a cell | Same as {{< key "Enter" >}}: edit the cell, or quick look on a read-only grid. |
| Click in a SQL editor | Places the cursor (with a split editor, also focuses the editor you clicked). Click the results to focus them. |
| Click in the console or the row-detail pane | Focuses it (and in the console selects the line). The row-detail pane takes the focus only when the tab has rows to show. |
| Right click | Opens the menu for what is under the pointer: an object in the sidebar, the console, a tab in the tab bar, a column header (column menu), the filter bar, a grid cell (cell menu) or a query result (result menu). |

Clicks do nothing while a dialog is open and on the connection list (the wheel still scrolls, since it is sent
as arrow keys). Clicks and right clicks in the main area are ignored while the pane is split
(see [Panes](../panes/)); in the tab bar only the first pane's tabs answer a click. Two clicks count as a double
click when they land on the same cell within 450 ms. The web UI is a normal web page: every control is clickable, and right click opens
the same menus.

## Themes

Both UIs take their colours from `[theme]`: a built-in palette by `name` (`dark`, `light`, `auto`,
`silver-dark`, `jade-dark`, `polaris-dark`, `sunset-dark`, `sunset-light`), a theme file, and
`[theme.colors]` overrides for single keys such as `accent`, `sidebar_bg`, `tabs_bg` or `window_bg`. `auto`
follows the terminal's background in the terminal and `prefers-color-scheme` in the browser. See
[Themes](../../configuration/themes/).

## Settings

| Setting | Effect |
|---|---|
| `[general] hints` | The actions shown in the terminal status line, in order (terminal only). |
| `[web] toolbar` | The buttons of the web toolbar, in order (web only). |
| `[general] show_connection_name`, `show_database_name` | What a workspace is called in the workspace list and the web switcher: the connection name, the database name, or both (the default when neither or both are set). |
| `[theme]` | Colours; see above. |

See the [settings reference](../../configuration/settings-reference/).
