---
title: Glossary
description: The words webtuidb uses in its two UIs and in these docs, one line each.
weight: 80
---

The terms used across webtuidb and this documentation, with the page that explains each in full.

| Term | Meaning |
|---|---|
| **Action** | A named command (`commit`, `filter_add_copy`) that a key, a menu entry or Open anything runs; the name is what `[keys]` rebinds. See [Actions](../actions/). |
| **Prefix** | The key pressed before an action key, tmux-style: {{< key "C-d" >}} by default, set with `[general] prefix`. Pressing it twice sends the key itself. |
| **No-prefix key** | A binding that works without the prefix, like tmux `bind -n`: the pane focus keys {{< key "C-h" >}} … {{< key "C-l" >}}, the filter bar's {{< key "M-n" >}}, the console's `/`. Written `-n KEY` under `[keys]`. |
| **Which-key panel** | The list of valid keys shown after you press the prefix; {{< key "Esc" >}} cancels it. |
| **Context** | The pane a binding applies in: `global`, `sidebar`, `grid`, `editor`, `results`, `structure`, `filterbar`, `console`. The focused context is searched first. |
| **Connection** | A saved set of driver, host, credentials and options in `connections.toml`, or a URL given on the command line. |
| **Tag** | A connection's label (local, development, testing, staging, production, or your own) that colours its badge. |
| **Session** | One live connection to a server, opened from a saved connection. {{< key "C-d n" >}} opens another; {{< key "C-d l" >}} switches between them. |
| **Workspace** | The screen of one session and one database: sidebar, tabs, panes, console. Several can be open; {{< key "C-d }" >}} / {{< key "C-d {" >}} move between them, {{< key `C-d \` >}} lists them. |
| **Sidebar** | The left pane listing the database's objects, saved queries and history. |
| **Tab** | One table, editor, diagram, board or assistant view inside a workspace. |
| **Pane** | One region of the screen: sidebar, the tab area, row detail, console. A split shows two tab groups side by side or stacked. |
| **Zen mode** | `toggle_panels` ({{< key "C-d M-0" >}}): sidebar, console, row detail and status bar hidden. |
| **Open anything** | The searchable list of every object, saved query, database, feature and action ({{< key "C-d p" >}}). |
| **Grid** | A table's data tab: rows and columns you browse and edit. |
| **Quick look** | A pop-up with one cell's full value: JSON pretty-printed, hex, images, geometry as WKT ({{< key "C-d Enter" >}} in the grid). |
| **Row detail** | A side pane with every column of the selected row ({{< key "C-d Space" >}}). |
| **Filter bar** | The row of column conditions above the grid ({{< key "C-d f" >}}); applied filters become a `WHERE` clause. |
| **Quick filter** | A filter made from the cell under the cursor ({{< key "C-d x" >}}). |
| **Pending change** | An edit, insert or delete made in a grid but not yet sent to the database: shown coloured or struck through until you commit or discard. |
| **Preview** | The SQL that committing would send, shown before anything runs ({{< key "C-d P" >}}). |
| **Commit** | Sending all pending changes to the database, in one transaction where the engine supports it ({{< key "C-d s" >}}). |
| **Discard** | Dropping all pending changes ({{< key "C-d Backspace" >}}). |
| **Safe mode** | A connection's guard level for statements sent to the server: **Silent** (no question), **Alert 1** (confirm every statement), **Alert 2** (confirm all but SELECT / EXPLAIN / SHOW), **Safe 1** (password for every statement), **Safe 2** (password for all but reads). See [Safety](../../safety/). |
| **Read-only connection** | A connection with `read_only = true`: data-changing statements are refused by the database session. |
| **Passcode lock** | A screen that hides webtuidb until the passcode is typed (`[security] passcode`, {{< key "C-d M-L" >}}). |
| **SQL editor** | A query tab with the editor and its results ({{< key "C-d e" >}}). |
| **Statement** | One SQL command; the editor splits a script at `;` and runs the one at the cursor or all of them. |
| **Result tab** | One result of a run; a script that returns several results gets one tab each. |
| **Pinned result** | A result tab kept when you run again ("Pin result" in the result menu). |
| **Pinned connection** | A server connection reserved for one editor tab so `BEGIN`, `SET` and temporary tables carry across runs. |
| **Editable result** | A result from a plain single-table `SELECT` with the primary key, which you can edit like a grid. |
| **Variables / parameters** | Placeholders such as `:id` in a statement, filled in a dialog before it runs ({{< key "C-d M-v" >}}). |
| **Saved query** | A `.sql` file in the queries folder, listed in the sidebar and in Open anything. |
| **Shared folder** | An extra, read-only saved-query folder (`[queries] shared_dirs`). |
| **History** | Every statement run on a connection, newest first, kept in `history/<connection>.jsonl`. |
| **Console** | The log of the statements a workspace sent, with timing and errors ({{< key "C-d C-c" >}}). |
| **Structure** | A table's columns, indexes, foreign keys and triggers, edited in the structure view ({{< key "C-d )" >}}). |
| **DDL** | The `CREATE` statement of an object ({{< key "C-d M-d" >}}). |
| **Item group** | A custom sidebar group of objects, made from marked items. |
| **Mark** | Adding a sidebar item to a multi-selection (`space` in the sidebar). |
| **Job** | A background task: import, export, backup, restore, search or diff, listed in the jobs centre ({{< key "C-d M-J" >}}). |
| **Board** | A metrics board: panels of read-only queries shown as numbers, tables or charts, refreshed on a timer ({{< key "C-d m" >}}). |
| **ER diagram** | The tables and their foreign keys drawn as boxes ({{< key "C-d d" >}}). |
| **Assistant** | The AI chat that can read the schema and propose SQL ({{< key "C-d A" >}}). |
| **MCP server** | webtuidb serving its tools to AI clients over the Model Context Protocol (`--mcp`, `[mcp]`). |
| **Plugin** | A TOML command or JavaScript file in the plugins folder, run on the current object or result; its action is `plugin:<name>`. |
| **Bootstrap** | Statements a connection runs right after connecting (`bootstrap` in `connections.toml`). |
| **Front end** | One of the two UIs: the terminal UI, or the web UI served by `webtuidb --frontend`. |
| **Token** | The secret the web UI uses to call the server, printed once as `#token=…` in the address. |
| **Data folder / config folder** | `~/.local/share/webtuidb` and `~/.config/webtuidb`. See [Files](../files/). |
