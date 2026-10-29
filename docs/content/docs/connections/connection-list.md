---
title: Connection list
description: The terminal welcome screen and the web start page, where you pick, filter, sort, group, copy and manage saved connections.
weight: 10
---

The connection list is the first thing you see: in the terminal it is the welcome screen, in the browser the start page. It lists every saved connection, grouped by folder, and is where you connect, create, edit, duplicate, delete, test and copy connections.

{{< ui tui="connections-list" web="connections-list" >}}The connection list with the demo connections: two groups, a tag badge per connection and the last-connected time.{{< /ui >}}

## How to open it

| | Terminal | Web |
|---|---|---|
| At start | `webtuidb` with no argument | the URL `webtuidb --frontend` prints |
| From a workspace | {{< key `C-d \` >}} (`workspaces`), then **‹ connection list ›** | {{< key `C-d \` >}} or the connection name in the top bar, then **All connections** |
| Back to the open workspace | {{< key "Tab" >}} | **Resume** on the connection's row |

## The terminal welcome screen

The screen has three parts, top to bottom: the wordmark and a short reminder of the prefix key, the engine strip, and the **Connections** frame. When the terminal is at least 118 columns wide a **Quick start** frame sits to the right with the main keys and the paths of `connections.toml` and `config.toml`. On a short terminal the wordmark shrinks to one line and the engine strip is dropped so the list keeps its room.

### Engine strip

One tile per driver, plus **All** first. {{< key "←" >}} / {{< key "→" >}} (or {{< key "h" >}} / {{< key "l" >}}) move the highlighted tile. The highlighted engine does two things:

- it filters the list to that driver; the frame title becomes, for example, **Postgres connections (2 of 9)**;
- it pre-fills **Driver** when you press {{< key "n" >}}.

When the tiles do not fit, `‹ 3` and `› 3 more` at the end of the strip line say how many are off screen.

{{< ui tui="connections-engine-filter" >}}The Postgres tile is highlighted, so only PostgreSQL connections are listed.{{< /ui >}}

### Columns

| Column | What it shows |
|---|---|
| (stripe) | a bar in the driver's colour leads each row, so a mixed list scans by engine |
| NAME | the connection name, indented under its group; the selected row is bold and marked `▸` |
| DRIVER | the driver name in its colour |
| HOST | `host:port`, plus ` via ssh` when the connection has an SSH tunnel. This column shrinks first on a narrow terminal |
| LAST | when you last connected: `now`, `12m ago`, `5h ago` (up to 48 hours), `3d ago` (up to 60 days), then the month (`2026-01`); empty if never |
| TAG | the tag as a coloured badge (the connection's own colour wins over the tag's), a plain colour swatch when there is a colour but no tag, ` RO` for a `read_only` connection and ` source` for one that comes from a [connection source](../tags-colours-groups/#connection-sources) |

Group headers (`▾ Shop`, `▾ Shop/Reporting`) separate the groups; connections without a group come first. Groups are always shown expanded: neither UI has a fold key for them. The frame title counts all connections and adds `· 2 open` when workspaces are open.

### Keys on the welcome screen

These keys work without the prefix, because the welcome screen has no text field to type into.

| Key | What it does |
|---|---|
| {{< key "j" >}} {{< key "k" >}} / {{< key "↓" >}} {{< key "↑" >}} | move between connections (group headers are skipped) |
| {{< key "←" >}} {{< key "→" >}} / {{< key "h" >}} {{< key "l" >}} | pick an engine tile |
| {{< key "Enter" >}} | connect to the selected connection |
| {{< key "/" >}} | search |
| {{< key "n" >}} | new connection (driver from the engine tile, Postgres on **All**) |
| {{< key "e" >}} | edit the selected connection |
| {{< key "c" >}} | duplicate it |
| {{< key "d" >}} or {{< key "Delete" >}} | delete it (asks first) |
| {{< key "i" >}} | import a connection from a URL |
| {{< key "u" >}} | copy it as a URL without the password |
| {{< key "U" >}} | copy it as a URL with the password (asks first) |
| {{< key "g" >}} | groups: rename or delete one |
| {{< key "s" >}} | sort by name, tag, driver or recent |
| {{< key "m" >}} | the connection menu |
| {{< key "x" >}} | export connections to a file |
| {{< key "I" >}} | import connections from a file |
| {{< key "Tab" >}} | back to the open workspace, if there is one |
| {{< key "Esc" >}} | cancel every connection attempt still in progress |

The global prefix keys still work here: {{< key "C-d ?" >}} for help, {{< key "C-d K" >}} to pick a connection from a list, {{< key "C-d q" >}} to quit. Workspace actions such as {{< key "C-d k" >}} answer `connect first: Enter on a connection`.

### Search

{{< key "/" >}} opens a search line above the list. Typing filters the list by connection name, host and tag at once. {{< key "↑" >}} / {{< key "↓" >}} move through the matches while you type. {{< key "Enter" >}} closes the search line and keeps the filter; {{< key "Esc" >}} clears it and keeps the same connection selected.

{{< ui tui="connections-search" >}}Searching for "shop" keeps the five shop connections.{{< /ui >}}

The web start page has no search box: use the browser's own find ({{< key "C-f" >}}), or [Open Anything](../../workspace/) once connected.

## The web start page

The page is headed **Connections**, with these buttons on the right:

| Button | What it does |
|---|---|
| **Open file…** | read a `.sql` file in the SQL viewer without connecting |
| **Settings** | the settings dialog |
| **Import…** | [import connections](../import-export-connections/) from a file |
| **Export…** | [export connections](../import-export-connections/) (disabled when there are none) |
| **Add connection** | the [connection form](../connection-form/) for a new PostgreSQL connection |

Under the heading:

- **Paste a connection URL** and **Continue** (or {{< key "Enter" >}}) parse the URL on the server and open the form filled in. See [Connection URLs](../connection-urls/).
- One button per engine (**PostgreSQL**, **MySQL** … **MongoDB**) opens the form for a new connection of that driver.
- **N connections** and **Sort by** appear once there are two or more connections.

Each connection row shows a colour bar (the connection's colour, else its tag's), the engine logo, the name, chips for **● open** (a workspace for it is open in this browser tab), the tag, **read only** and **source**, and on the second line the engine, `host:port/database` and **connected 5 minutes ago**. On the right are the **⋯** menu button and **Connect**, which reads **Resume** when the workspace is already open and **Connecting…** while it opens.

Clicking anywhere on a row connects, as does {{< key "Enter" >}} on a focused row. Right-click, the {{< key "ContextMenu" >}} key or {{< key "S-F10" >}} open the row menu.

### Moving a connection to another group

Drag a row onto a group header to move the connection into that group. While you drag, a strip **Drop here to take it out of its group** appears above the ungrouped connections. Connections from a connection source cannot be dragged.

### When a connection fails

A failed connect or test shows a box with **Could not connect to *name***, one plain sentence about what to check, and three controls: **Edit connection…** (not for source connections), **Try again**, and **Technical details** with the driver's full error.

| The driver's error mentions | The sentence shown |
|---|---|
| connection refused | Nothing answered at that address. Check that the database server is running, and that the host and port are right. |
| no such host, name resolution | That host name could not be found. Check the spelling of the host. |
| password authentication failed, access denied, SQLSTATE 28P01 / 28000, MySQL 1045 | The user name or password was not accepted. |
| does not exist, unknown database, SQLSTATE 3D000 | That database does not exist on the server. Check the database name. |
| timeout, deadline exceeded | The server did not answer in time. Check the host and port, and that the network or VPN is up. |
| ssl, tls, certificate, x509 | The secure connection could not be set up. Check the SSL settings of this connection. |
| permission denied, insufficient privilege | This user is not allowed to do that on the server. |
| anything else | Check the connection details and try again. |

The terminal shows the driver's error in a **Connection failed** dialog instead; {{< key "Esc" >}} or {{< key "q" >}} closes it.

## Sort by

{{< ui tui="connections-sort" web="connections-sort" >}}Sort by: name, tag, driver or recent.{{< /ui >}}

| Order | Meaning |
|---|---|
| name | alphabetical, ignoring case (the default) |
| tag | in the order of the tag list: `local`, `testing`, `development`, `staging`, `production`, then your `[[tag]]` entries; untagged last |
| driver | by driver name |
| recent | most recently connected first; never-connected last |

Sorting applies inside each group; the groups themselves stay in path order. Ties fall back to the name. The terminal opens the picker with {{< key "s" >}} (the current order is marked `•`) or **Sort by…** in the connection menu; the web uses the **Sort by** dropdown or **Sort by…** in the row menu. Your pick is remembered in `~/.local/share/webtuidb/state.json` and shared by both UIs; until you pick one, `[general] connection_sort` decides.

## The connection menu

{{< ui tui="connections-row-menu" web="connections-row-menu" >}}The connection menu: {{< key "m" >}} in the terminal, the ⋯ button or right-click in the browser.{{< /ui >}}

Both UIs show the same entries in the same order:

| Entry | What it does |
|---|---|
| Connect | open a workspace (web: resume it if it is open) |
| Edit… | the [connection form](../connection-form/) |
| Duplicate | a copy named `<name> copy` (`<name> copy2` when that is taken), saved at once. A copy of a source connection is a normal, editable one |
| Delete… | remove it from `connections.toml` after a confirmation; the database is not touched |
| Test | connect, report the server version, disconnect |
| Copy URL | the connection as a URL without the password |
| Copy URL with password | the same with the password, after a confirmation |
| Import URL… | terminal: the import dialog; web: focuses the paste box and says **Paste a connection URL and press Enter.** |
| Export connections… | the export dialog |
| Import connections… | the import dialog |
| Sort by… | the sort picker |

For a connection from a connection source, **Edit…** and **Delete…** are refused: the terminal says `<name> comes from <file>: edit that file (duplicate makes a local copy)`, the web greys the entries out with the same reason.

### Delete

{{< ui tui="connections-delete" web="connections-delete" >}}Deleting asks first. In the terminal only {{< key "y" >}} confirms.{{< /ui >}}

The terminal asks **Delete connection *name*?** with the note that only `connections.toml` changes. As in every destructive dialog of the terminal UI, {{< key "Enter" >}} does not confirm: press {{< key "y" >}}; {{< key "Enter" >}}, {{< key "Esc" >}} and {{< key "n" >}} cancel. The web asks **Delete *name*?** with a **Delete** button.

### Copy as URL

{{< key "u" >}} and **Copy URL** put the connection on the clipboard as a URL without the password, because clipboards are often shared and synced. {{< key "U" >}} and **Copy URL with password** first ask **Copy the URL with the password to the clipboard?** with the note that clipboard managers keep what is copied. The URL format is described in [Connection URLs](../connection-urls/#copy-as-url).

{{< ui tui="connections-copy-url-password" web="connections-copy-url-password" >}}Copying with the password asks first.{{< /ui >}}

The terminal copies through the system clipboard or, over SSH, the OSC 52 escape. The web uses the browser clipboard and says **Could not copy to the clipboard. Select the text and copy it yourself.** when the browser refuses.

## Groups

A connection's **Group** (`group = "Shop"`) puts it under a header; `A/B` makes a sub-group shown as its own header `A/B`. See [Tags, colours and groups](../tags-colours-groups/#groups).

{{< ui tui="connections-group-menu" >}}{{< key "g" >}}, pick a group, then Rename… or Delete the group and its connections….{{< /ui >}}

**Terminal.** {{< key "g" >}} lists every group path with the number of connections in it, sub-groups included (`no groups yet: give a connection a Group in its form` when there are none). Type to filter, {{< key "Enter" >}} to pick, then:

- **Rename…** asks for the **New path** (`A/B` nests). Every connection in the group and its sub-groups moves: renaming `Shop` to `Retail` turns `Shop/Reporting` into `Retail/Reporting`. Connections from a connection source stay where their file puts them. The status line says `moved 3 connections to Retail`.
- **Delete the group and its connections…** lists the connections it removes (the group and every sub-group) and asks you to **Type the group name**. If any of them is open with unsaved changes, a `⚠ Open and closed with it, discarding unsaved changes` line names them; their workspaces close too. While one of them is still sending a commit, the form refuses with `<name> is still sending a change: wait for it to finish`.

**Web.** Each group header shows the name, the number of connections directly in it, and **Rename** / **Delete** buttons (brighter on hover). **Rename** asks for the new name; **Delete** asks you to `Type Shop to delete this group and its 3 connections`, and a mismatch answers **The name did not match; nothing deleted.** Both refuse when the group holds a connection from a connection source. As in the terminal, deleting `Shop` also removes `Shop/Reporting`.

{{< ui tui="connections-group-delete" web="connections-group-delete" >}}Deleting a group asks you to type its name.{{< /ui >}}

{{< callout type="warning" >}}
Deleting a group deletes every connection in it and in its sub-groups from `connections.toml`. The databases are untouched, but the saved settings are gone; export the group first if you may want it back.
{{< /callout >}}

## Connection details

Once connected, {{< key "C-d M-K" >}} (`connection_details`) shows what the workspace is connected to. In the web, the **details** button in the status bar opens the same dialog.

{{< ui tui="connections-details" web="connections-details" >}}Connection details for Shop (Postgres).{{< /ui >}}

| Row | Terminal | Web |
|---|---|---|
| Name / Connection | ✓ | ✓ |
| Driver | ✓ | ✓ |
| Server / Server version | the version the server reported | ✓ |
| Host | `host:port` (the driver's default port when none is set) | ✓ |
| Database | the database open now | named after the engine's noun (Database, Keyspace, Region…) |
| Schema | | ✓ |
| User | ✓ | ✓ |
| TLS | `off`, or `on (require)` | the SSL mode (`default` when unset), plus `through an SSH tunnel` |
| Tag | | ✓ |
| Safe mode | the current level | ✓ |
| Read only | yes / no | |
| Encoding | | when set |
| Row limit | | ✓ |
| Colour | the status colour, or `(the tag's)` | swatches |
| SSH | `user@host:port` when tunnelled | |
| SOCKS | the proxy when set | |
| Source | the source file, for a source connection | |

Both let you change the connection's status colour from here: {{< key "o" >}} in the terminal opens the colour picker, the web dialog has swatches, a colour input, **Default** and **Save colour**. See [Status colour](../tags-colours-groups/#status-colour).

## Details and limits

- Every change on the list (add, edit, duplicate, delete, rename or delete a group, sort) is written to `connections.toml` immediately, with mode 0600. There is no undo.
- The terminal reads `connections.toml` once at start. If another program (or the web UI) adds a connection while it runs, a save from the terminal keeps that connection in the file, but the terminal's list only shows it after a restart. The web re-reads the file whenever it changed (see [connection sources](../tags-colours-groups/#connections-toml-reload)).
- Two browser tabs editing the list at once: the second save is refused and the page says **The connection list changed elsewhere. It was reloaded: make your change again.**
- The web lists connections in the same groups, but it has no engine filter, search or keyboard shortcuts for the list itself; on the start page the prefix keys open only `open_file`, `open_recent`, `settings` and `help`.
- Neither list has multi-select: each action applies to one connection, or to a whole group from the group commands.

## Settings

| Setting | Default | Effect |
|---|---|---|
| `[general] connection_sort` | `"name"` | the order before you pick one with Sort by: `name`, `tag`, `driver` or `recent` |

See the [configuration reference](../../configuration/).
