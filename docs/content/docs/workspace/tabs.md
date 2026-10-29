---
title: Tabs and workspaces
description: Tab kinds, opening, closing, moving and renaming tabs, the tab menu, the reuse rule, restoring tabs, workspaces and go back / forward.
weight: 30
---

Each thing you open in a workspace is a tab: a table, a SQL editor, the assistant. Tabs sit in a bar on top of
the main pane (one bar per pane when it is split). Several connections open at once are several workspaces.

{{< ui tui="workspace-tab-menu" web="workspace-tab-menu" >}}The tab menu of the `orders` tab.{{< /ui >}}

## Tab kinds

| Kind | Terminal | Web |
|---|---|---|
| Table | `▦ orders` (a view shows the sidebar's view icon). Views inside: Data, Structure ({{< key "C-d )" >}}), Index ({{< key "C-d M-i" >}}), DDL ({{< key "C-d M-d" >}}), and Json / Tree ({{< key "C-d J" >}}, {{< key "C-d T" >}}) on MongoDB and Redis. | `orders`, with the schema in front when it is not the current one. The same views from the toolbar's view menu. |
| SQL editor | `≡ SQL 1`, or the file or saved-query name. `●` when the `.sql` file it came from has unsaved changes, `[pinned]` when the tab has its own pinned connection, `[tx]` while it holds an open transaction. | `Query 1`, or the saved query's name once saved. |
| AI assistant | One per workspace: `AI assistant` (`· provider` when another provider is picked). | **Assistant**, one per workspace. |
| ER diagram, metrics boards | Full-screen windows, not tabs. | **Diagram** and **Metrics** tabs, one of each. |
| SQL viewer | A read-only window for a `.sql` file opened from the connection list. | The same, from the connections page. |

The terminal numbers the tabs of each pane from 1 (`1 ≡ SQL 1   2 ▦ customers`), cuts titles at 24 cells and adds
`●` to a tab with pending edits. The web shows `●` on the close button of such a tab.

## Keys

| Action | Key | What it does |
|---|---|---|
| `new_tab` | {{< key "C-d t" >}} | A new tab: a SQL editor, or with `[general] new_tab_opens = "empty"` the sidebar takes the keys instead (the web also shows the sidebar and focuses its filter box). |
| `new_query_tab` | {{< key "C-d e" >}} | A new SQL editor. In the web also **+** at the end of the tab bar, or a double click on the empty part of the bar. |
| `close_tab` | {{< key "C-d w" >}} | Close the active tab (asks when it has something to lose, see below). |
| `next_tab`, `prev_tab` | {{< key "C-d ]" >}}, {{< key "C-d [" >}} | Next / previous tab of the focused pane, wrapping. |
| `tab_1` … `tab_9` | {{< key "C-d 1" >}} … {{< key "C-d 9" >}} | Go to that tab of the focused pane. |
| `move_tab_left`, `move_tab_right` | {{< key "C-d M-{" >}}, {{< key "C-d M-}" >}} | Swap the tab with its neighbour in the same pane (`this tab is already the first one`). |
| `rename_tab` | {{< key "C-d M-n" >}} | Rename the tab: **Rename tab**, *Name (empty restores it)*. |
| `close_other_tabs` | {{< key "C-d M-w" >}} | Close the other tabs of this pane. |
| `close_tabs_right` | {{< key "C-d M-W" >}} | Close the tabs to the right of this one, in this pane. |
| `close_all_tabs` | {{< key "C-d M-A" >}} | Close every tab, in both panes. |
| `go_back`, `go_forward` | {{< key "C-d <" >}}, {{< key "C-d >" >}} | Reopen the table you had open before / after. |

In the sidebar {{< key "C-d M-n" >}} is `new_schema`, so rename a tab with the main pane focused. Clicking a tab
shows it in both UIs (see [Mouse](../layout/#mouse-in-the-terminal)). In the web a focused tab also takes
{{< key "Enter" >}} (show it), {{< key "Shift+F10" >}} or the menu key (its menu) and {{< key "M-S-Left" >}} /
{{< key "M-S-Right" >}} (move it), and you can drag a tab onto another: it lands before it, or after it when you
drop on the right half; dropping it on a tab of the other pane moves it there.

## The tab menu

Right click a tab (terminal and web), use the **▾** on a tab (web), or pick **Tab menu** in
[Open Anything](../open-anything/) (terminal).

| Entry | What it does |
|---|---|
| Close | `close_tab` |
| Close other tabs | `close_other_tabs` |
| Close tabs to the right | `close_tabs_right` |
| Close all tabs | `close_all_tabs` |
| Rename tab… | `rename_tab` |
| Move left / Move right | `move_tab_left` / `move_tab_right` |
| Open workspace in a new browser tab | Web only: the same session in another browser tab (the access token goes through browser storage, not the URL). The terminal lists it greyed: `web only: the terminal has one window`. |
| Copy link | Web only: a link to this tab. A table tab copies `/s/<session>/t/<schema>/<table>` with the filters applied now, a saved-query tab `/s/<session>/q/<path>`, any other tab the workspace link. The link carries no token, so whoever opens it still needs one. |

## Closing a tab that has something to lose

{{< ui tui="workspace-tab-close" web="workspace-tab-close" >}}Closing a table tab with a pending edit.{{< /ui >}}

| The tab has | Terminal asks | Web asks |
|---|---|---|
| Pending edits | `Close orders with unsaved changes?` *Pending edits in this tab will be lost.* **Close** | `Discard pending changes in the tab(s) being closed?` **Discard** |
| An open transaction | `This tab has an open transaction. Close it and roll back?` **Roll back and close** | the same |
| A modified `.sql` file | `Save changes to report.sql?` **Save** / **Don't save** / **Cancel** | (files are not written back in the web) |

Closing several tabs at once asks once: `Close 2 tabs with unsaved changes?` and then reports `closed 3 tabs`.
Quitting, switching database and disconnecting ask the same questions. In the web, reloading or closing the
browser tab while a tab has pending edits or an open transaction brings up the browser's own "leave site?"
warning.

## Tab reuse

Opening a table from the sidebar, Open Anything or go back / forward follows one rule in both UIs:

1. If that table is already open in the focused pane, its tab is shown.
2. Otherwise, if the active tab is a table tab with no pending edits, it is replaced by the new table.
3. Otherwise (the active tab is an editor, has edits, or is another kind) a new tab opens.

**Open in new tab** ({{< key "o" >}} in the terminal sidebar, a double click in the web sidebar,
{{< key "M-Enter" >}} in Open Anything) always opens a new tab. In the web a Search in database hit and a foreign-key jump always open a new tab; the
terminal opens a search hit through the reuse rule. Either way a tab with pending edits is never replaced.

## Go back and forward

Every table, view or materialized view you open is recorded, up to 50. {{< key "C-d <" >}} reopens the previous
one and {{< key "C-d >" >}} the next, through the reuse rule above; opening something new drops the forward
part. At either end: `nothing to go back to` / `nothing to go forward to`. Switching database clears the trail.

## Tabs that come back

| | Terminal | Web |
|---|---|---|
| After a reload of the page | — | Every tab of the session comes back from the browser's session storage: tables with their filters and view, editors with their text, names, panes and the active tab of each. A tab whose first line is `-- sensitive` is never stored, and an editor's stored text is capped at 64 KB. |
| On the next connect | `[editor] auto_save_tabs`: `first` (default) restores the first non-empty SQL editor, `all` every non-empty SQL editor, `none` nothing. The text, the tab's name and the saved query it saves into are kept; saved when the workspace closes. | A new session reads the same list. With `all` the web also stores and reopens table tabs (the terminal skips those); it saves shortly after each change. |
| On the next start | `[general] reopen_workspaces` (default on) reopens the connections that were open when you quit, each on the database it showed, unless you start with a connection named on the command line. | The start page reopens the last workspaces once per browser tab. That list lives in the browser's local storage, so another browser or port starts empty. |

A table tab also remembers, per table and database in `state.json`, its filters, sort, hidden columns and column
widths; see [Browsing data](../../browsing-data/).

## Workspaces

Each connection you open is a workspace with its own sidebar, tabs and console. The terminal shows
`workspace 2/3` on the right of the top bar when there is more than one.

{{< ui tui="workspace-workspaces" web="workspace-workspaces" >}}Two workspaces open: the terminal's Workspaces picker and the web's connection switcher.{{< /ui >}}

| Action | Key | Terminal | Web |
|---|---|---|---|
| `workspaces`, `switch_session` | {{< key `C-d \` >}}, {{< key "C-d l" >}} | **Workspaces**: each workspace (`connection / database`, the driver, `● current`), then `‹ connection list ›` and `‹ close other connections ›`. | The connection switcher. |
| `next_workspace`, `prev_workspace` | {{< key "C-d }" >}}, {{< key "C-d {" >}} | Cycle through the workspaces. | Cycle through the sessions open in this browser tab (`Only one workspace is open`). |
| `open_connection` | {{< key "C-d K" >}} | **Open connection**: pick a saved connection. | The connection switcher. |
| `new_session` | {{< key "C-d n" >}} | Pick a connection, then its database (offered when there is more than one). | The same. |
| `close_other_connections` | {{< key "C-d M-Q" >}} | Closes every other workspace; refuses while one is still sending a change, asks when one has pending edits. | Asks `Close 2 other connections?` |

Switching sessions, databases and connections is covered in
[Sessions and databases](../../connections/sessions-and-databases/).

## Settings

| Setting | Default | Effect |
|---|---|---|
| `[general] new_tab_opens` | `query` | `query` or `empty`: what `new_tab` opens. |
| `[editor] auto_save_tabs` | `first` | `none`, `first` or `all`: which tabs come back on the next connect. |
| `[general] reopen_workspaces` | `true` | Reopen the last workspaces at start. |
| `[general] show_connection_name`, `show_database_name` | off | How workspaces are labelled: connection, database, or both. |
