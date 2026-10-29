---
title: Saved queries
description: Keep queries as plain .sql files with a name, keywords and folders; open, run, rename, move and delete them; share read-only folders; expand them from autocomplete.
weight: 110
---

A saved query is a plain `.sql` file in your data folder. Its first lines can give it a name and
keywords; folders group them. Both UIs list the same files, so a query saved in the terminal is in the
browser at once. Open one to edit it (the file is updated as you type), run it straight from the list,
or type one of its keywords in the editor to insert it.

{{< ui tui="results-queries" web="results-queries" >}}The demo's saved queries: two at the top level and two in the `reports` folder.{{< /ui >}}

## How to open it

| Terminal | Web |
| --- | --- |
| The sidebar's **2 Queries** tab: focus the sidebar ({{< key "C-d M-h" >}}), then {{< key "2" >}}, or {{< key "Tab" >}} to cycle the tabs | the **Saved queries** button in the top bar (on a narrow window, in the **☰** menu); **✕** closes the drawer |

Saved queries are also in Open Anything ({{< action "open_anything" >}}), listed as `saved query` (terminal)
or `query` (web), so you can open one by typing part of its name.

## The file format

```sql
-- name: Monthly revenue
-- keywords: report, chart
-- barchart x: month, y: revenue
SELECT date_trunc('month', placed_at)::date AS month, sum(total) AS revenue, count(*) AS orders
FROM orders
WHERE status <> 'cancelled'
GROUP BY 1
ORDER BY 1;
```

| Line | Meaning |
| --- | --- |
| `-- name: …` | the name shown in the lists. Without it the file name (minus `.sql`) is the name |
| `-- keywords: a, b` | comma-separated words that insert this query from autocomplete |
| anything else | the query, from the first line that is not one of the two headers |

Only the leading `-- name:` and `-- keywords:` lines are headers (any order, any case); other comments,
like the `-- barchart` line above, are part of the query.

Files live under `~/.local/share/webtuidb/queries/` (under `$XDG_DATA_HOME` when set). A query named
"Monthly revenue" in the folder `reports` is `queries/reports/Monthly_revenue.sql`: characters other than
letters, digits, `.`, `_` and `-` become `_` in the file name. You can add, edit and remove files there
by hand; both UIs read the folder each time they list it.

## Saving a query

| | Terminal | Web |
| --- | --- | --- |
| From the editor | {{< key "C-d a" >}} (`add_to_queries`): form **Add to saved queries** with **Name**, **Keywords** (comma-separated) and **Folder** | **Save query** on the toolbar, or {{< key "C-d a" >}}: asks "Save query as" (a name) |
| What is saved | the selection, else the statement under the cursor | the whole editor text |
| From the history | **Add to saved queries…** in the entry menu (same form) | **Add to saved queries…** in the entry menu (asks a name) |
| Name taken | "Overwrite saved query X?" (**Overwrite**) | "A saved query "X" already exists. Replace it?" (**Replace**) |

{{< ui tui="results-queries-add" web="results-queries-add" >}}Saving the current statement as a query.{{< /ui >}}

A name with a slash saves into a folder: `reports/By country` is "By country" in `reports` (in the
terminal it goes below whatever **Folder** says). A name cannot be empty (`a saved query needs a name`),
and neither a name nor a keyword can contain a line break. Two names that make the same file name are
refused: `a different saved query "a b" already uses the file name a_b.sql`.

The browser cannot set keywords; add them in the terminal or in the file. Replacing an existing query
from the browser writes it without keywords.

### Editing a saved query

Opening a saved query in the terminal ties the tab to the file: with `[editor] auto_save_queries = true`
(the default) every change is written back as you type, keeping the name and keywords. When you save the
whole buffer with {{< key "C-d a" >}}, that tab keeps auto-saving to the new query too.

In the browser a tab opened from the drawer is not tied to the file. Once you save the tab with
**Save query** (or `:w` in Vim mode), it is, and auto-save writes it one second after you stop typing.

## The list

| | Terminal | Web |
| --- | --- | --- |
| Header | `Saved queries · 4` | **Saved queries**, with **Import…** and **Open file…** |
| Each entry | `≡` (`⇄` for a shared one), the folder, the name in bold, the keywords | the name (with `shared` when it is), **Reveal**, the start of the SQL, **Open**, **Run**, **⋯** |
| Folders | shown as `folder/` before the name | a header per folder with its count; click it to fold or unfold |
| Search | {{< key "/" >}} and type: fuzzy match on the name | — (use Open Anything) |

### Sort order

| Terminal | Web |
| --- | --- |
| {{< key "S" >}} on the Queries tab: **Sort saved queries by** name, created or modified | the **Sort by name / modified / created** box at the top of the drawer |

{{< ui tui="results-queries-sort" web="results-queries-sort" >}}Sort saved queries by name, created or modified.{{< /ui >}}

Folders always stay together (in folder order); inside a folder `name` sorts alphabetically, `modified`
newest first. The creation time of a file is not tracked, so `created` sorts like `modified`. The choice
lasts while the list is open.

## Using a query

| Do | Terminal | Web |
| --- | --- | --- |
| Open in a new tab | {{< key "Enter" >}}, {{< key "l" >}} or {{< key "o" >}} | click it, {{< key "Enter" >}}, or **Open** |
| Run it | open it, then {{< key "C-d Enter" >}} | **Run**: a new tab that runs it at once |
| Menu | {{< key "m" >}} | **⋯**, right-click, the {{< key "ContextMenu" >}} key or {{< key "S-F10" >}} |

{{< ui tui="results-queries-menu" web="results-queries-menu" >}}The saved query menu.{{< /ui >}}

| Entry | Does |
| --- | --- |
| Open | opens it in a new tab |
| Open in new tab | the same (every open is a new tab) |
| Insert into editor | types the query at the cursor of the current SQL editor (a new tab when none is open) |
| Rename… | asks a new name; the folder stays |
| Move to folder… | asks a folder (empty for the top level) |
| Reveal in file manager | opens the file's folder in Finder, Explorer or your file manager |
| Delete… | asks "Delete saved query X?" (terminal) / "Delete the saved query X?" (web), then deletes the file |

**Rename** reads the file again, saves it under the new name (keeping keywords and SQL) and then deletes
the old file, so the file name follows the new name. An existing name is refused: the terminal says
`a saved query named "x" already exists`, the browser `a saved query with this name already exists`.
Open terminal tabs that auto-save into the old file follow the rename. The two steps are not atomic: a
crash between them leaves both files.

**Move** differs between the UIs:

- The terminal (form **Move** with **Folder**, empty for the top level) moves the file and keeps its name.
  A file with the same name in the target folder is replaced without asking.
- The browser (and dragging, below) saves the query again in the target folder under its *file* name and
  deletes the old file. A query whose `-- name:` differs from its file name (the demo's
  `monthly-revenue.sql` is "Monthly revenue") comes out named after the file (`monthly-revenue`). A name
  that already exists there is refused.

**Reveal** in the browser opens the folder only when the browser runs on the same machine as the server;
from another machine it copies the file's path instead and says so. The browser also has a **Reveal**
button on each entry.

### Moving queries with the mouse (web)

Drag a saved query onto a folder header to move it into that folder; while you drag, a "Drop here to take
it out of its folder" strip at the top moves it to the top level. Shared queries cannot be dragged.

### Keywords in autocomplete

Every keyword of every saved query is an autocomplete candidate in both UIs. Accepting it replaces the
word with the query's SQL. A keyword written `snippet:sel` offers the query under `sel`, marked as a
snippet. Turning autocomplete off (`[editor] autocomplete`) turns this off too.

{{< ui tui="results-queries-keyword" web="results-queries-keyword" >}}Typing `look` offers the "Find customer" query by its keyword `lookup`.{{< /ui >}}

## Importing a .sql file as a query

| Terminal | Web |
| --- | --- |
| {{< key "i" >}} on the Queries tab: form **Import a .sql file as a saved query** with **File** | **Import…** in the drawer: pick a `.sql` file |

The terminal uses the file's `-- name:` and `-- keywords:` headers, else its file name; the browser names
it after the file (minus `.sql`). Both save it at the top level and ask before replacing a query with the
same name. To open a file without saving it, use {{< key "f" >}} on the Queries tab (form **Open a file in a
tab**) or **Open file…** in the drawer; see [SQL files](../sql-files/).

## Shared folders

`[queries] shared_dirs` lists extra folders whose `.sql` files are shown beside your own, read-only:

```toml
[queries]
shared_dirs = ["~/team/sql", "$HOME/projects/app/queries"]
```

- `~` and environment variables are expanded; relative paths are made absolute.
- Each folder's queries are grouped under the folder's own name (`sql/…`, `queries/…`), including its
  subfolders.
- They are marked `⇄` (terminal) or `shared` (web). Opening one never writes back to it.
- The terminal's menu leaves out Rename, Move and Delete for them; the browser greys them out with "a
  shared query is read only". The server refuses those writes too (`shared queries are read-only`).
- Only `.sql` files are read from a shared folder.

Use a shared folder for a team's queries kept in a git repository or a synced drive.

## Deep links (web)

`/s/<session>/q/<path>` opens the saved query at `<path>` (as listed, with its folders, for example
`/s/3fa9/q/reports/monthly-revenue.sql`) in a new tab of that session. **Copy link** in the tab menu of a
query tab that came from a saved query copies such a link. An unknown path says `No saved query …`. The
link holds no token; whoever opens it still needs access to the server. See [Web UI](../../web-ui/).

## The HTTP API

The browser talks to these endpoints (useful for scripts): `GET /api/queries` lists every query with an
`etag`; `GET /api/queries/<path>` returns one with an `ETag` header; `PUT /api/queries/<path>` with
`{"sql": …}` rewrites the SQL and keeps the name and keywords, and when it carries `If-Match: <etag>` it
fails with 409 (`this saved query changed elsewhere; reload it before saving`) if the file changed since; `POST /api/queries` saves a new one (409 `{exists: true}` unless
`overwrite`); `POST /api/queries/rename` renames or moves; `DELETE /api/queries/<path>` deletes one file
(never a folder); `GET /api/queries/export` downloads all your own queries as `queries.json`. See the
[API reference](../../reference/).

## Settings

| Setting | Effect |
| --- | --- |
| `[editor] auto_save_queries` | write an open saved query back to its file as you edit (default on) |
| `[queries] shared_dirs` | extra read-only folders of saved queries |
| `[editor] autocomplete` | autocomplete, including saved-query keywords |

See the [configuration reference](../../configuration/).
