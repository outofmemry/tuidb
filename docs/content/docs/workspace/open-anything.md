---
title: Open Anything
description: One search box for tables, saved queries, databases and every feature, with anchors, completion and open-in-new-tab.
weight: 50
---

Open Anything ({{< key "C-d p" >}}, `open_anything`) is a search box over everything you can open or run in
the workspace: the objects of the database, your saved queries, the other databases and the features of the app.
Type a few letters of the name and press {{< key "Enter" >}}.

{{< ui tui="workspace-open-anything" web="workspace-open-anything" >}}Open Anything before typing.{{< /ui >}}

## How to open it

| | |
|---|---|
| Terminal | {{< key "C-d p" >}} |
| Web | {{< key "C-d p" >}}, the **Open anything** toolbar button, or **Open anything** on an empty workspace |

## What it lists

Each line shows the name and, beside it, what it is.

| Group | Terminal | Web |
|---|---|---|
| Objects | Every object the sidebar has loaded, with its kind and schema (with `[sidebar] lazy_load = false`, those of every schema). | The objects of the current schema, with their kind and schema. |
| Saved queries | Every saved query (`saved query`). Opens it in a new SQL editor that saves back to its file. | Every saved query, with its folder. Opens it in a new SQL editor. |
| Databases | Every database on the server. Picking one switches this workspace to it. | The other databases. Picking one switches to it (after `Switching closes all open tabs. Continue?`). |
| Features | A fixed list of features (`feature`), below. | Every action the web supports, by its description (`action`), plus the features without a key (`feature`). |
| Plugins | Every plugin (`plugin`). | Plugins with a key, as actions. |

The terminal's feature list: New SQL editor, Open connection, New session, Switch session, Open database, Safe
mode, Key bindings, Reload, Process list, Disconnect, User management, ER diagram, Metrics board, Backup,
Restore, AI assistant, MCP activity, Run a plugin, Cancel running queries, Close other tabs, Close tabs to the
right, Close all tabs, Pin connection to this tab, Schema diff, Data diff, Fake data, Connection details, Item
overview, Rename tab, Clear console, Open a SQL file, Open a recent file, Settings, Close other connections,
Background jobs, Lock, Zen: hide panels, Move tab left, Move tab right, **New… (table, view, function, schema,
database, group)** (the sidebar's + menu), **Tab menu** (the menu of the active tab), **Open docs** and
**Enable SSH log** / **Disable SSH log**.

Several of these have no key binding and are only reachable here (or from a menu): Pin connection to this tab,
Schema diff, Data diff, Fake data, MCP activity, Item overview, Clear console, New…, Tab menu, Open docs and
the SSH log. The web lists Pin connection to this tab, Schema diff, Data diff, Fake data, MCP activity, Clear
console, Item overview, Open docs and Enable SSH log as features.

## Matching

{{< ui tui="workspace-open-anything-search" web="workspace-open-anything-search" >}}Typing `ord`: tables, views and features whose names contain those letters in order.{{< /ui >}}

| Typed | Matches |
|---|---|
| `ord` | Names containing the letters `o`, `r`, `d` in that order. The terminal ranks consecutive letters and letters at the start of a word higher and prefers shorter names (the sidebar's fuzzy match); the web lists names containing `ord` as one piece first (earliest position first), then the other in-order matches, tighter ones first. |
| `^ord` | Names that start with `ord`. |
| `ers$` | Names that end with `ers`. |
| `^orders$` | Exactly `orders`. |

Matching ignores case. The terminal's title is a reminder: `Open anything  (^name anchors the start, name$ the
end)`. The web shows the first 60 matches.

## Keys

| Key | Action |
|---|---|
| ↑ / ↓ (terminal also {{< key "C-p" >}} / {{< key "C-n" >}}) | Move the selection |
| {{< key "Enter" >}} | Open the selected entry |
| {{< key "M-Enter" >}} (terminal also {{< key "C-Enter" >}}) | Open a table or view in a new tab instead of through the [reuse rule](../tabs/#tab-reuse) |
| {{< key "Tab" >}} | Complete: replace what you typed with the selected name, so further letters narrow from there |
| {{< key "Esc" >}} | Close |

## Open docs and the SSH log

**Open docs** opens the project's README in your browser. The terminal runs `open` on macOS, `rundll32` on
Windows and `xdg-open` elsewhere; when that fails the status line shows the URL instead.

**Enable SSH log** (terminal) starts writing a trace of every new SSH tunnel to `ssh.log` in the data folder
(`~/.local/share/webtuidb/ssh.log`), and says `SSH log on: new tunnels are traced to <path>`; the entry then
reads **Disable SSH log**. It is the same trace as the `--ssh-log` flag. In the web the entry only explains
that the server has to be started with `--ssh-log`: a page cannot switch it on.

## Details

- Picking a database is the same as choosing it in the database picker: the workspace's tabs close (you are
  asked first when some have pending edits). See [Sessions and databases](../../connections/sessions-and-databases/).
- A feature runs exactly as its key or menu entry would, in the pane that had the focus before Open Anything
  opened.
- The terminal's object list is what the sidebar loaded; after creating a table elsewhere, reload the sidebar
  ({{< key "r" >}} in it, or {{< key "C-d r" >}}) to see it here.
