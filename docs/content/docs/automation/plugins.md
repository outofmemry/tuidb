---
title: Plugins
description: Add your own commands to webtuidb - a TOML file that runs any program with the current connection, table, selection and cell as JSON on stdin, or a JavaScript file that can query the database read-only, copy, open a table, notify and write SQL - with optional shortcuts, in the terminal and in the browser.
weight: 30
---

A plugin is a file in the `plugins` folder of the config directory that adds a command to webtuidb. There are two
kinds:

- a **command plugin** (`*.toml`) runs a program you name. webtuidb writes the context (connection, database,
  table, selection, cell) to its standard input as JSON and shows what it prints, or puts it in the SQL editor.
- a **JavaScript plugin** (`*.js`) runs inside webtuidb. It can run read-only queries on the open connection and
  ask the UI to copy text, open a table, show a notification or open SQL in a tab.

Both kinds can declare a shortcut and the panes they apply to. Both UIs run the same files.

{{< ui tui="automation-plugins" web="automation-plugins" >}}The plugins that apply to a table grid: `select-star` (a command plugin with SQL output), `show-context` (a command plugin) and `row-count` (a JavaScript plugin with the shortcut alt+P).{{< /ui >}}

## How to open it

| Action | Terminal | Web |
| --- | --- | --- |
| Plugins list (`plugins`) | {{< key "C-d M-p" >}}, or {{< key "C-d p" >}} → **Run a plugin** | {{< key "C-d M-p" >}}, or **Open anything** → **run a plugin** |
| One plugin by name | {{< key "C-d p" >}} → the plugin's name (detail `plugin`) | **Open anything** → `plugin: <name>` (only plugins with a shortcut are listed) |
| A plugin's shortcut (`plugin:<name>`) | {{< key "C-d" >}} then the plugin's key, for example {{< key "C-d M-P" >}} | the same sequence |

A plugin's action `plugin:<name>` can also go in `[web] toolbar` (a button in the browser's top bar) and in
`[general] hints`, like any other action.

## The plugins list

| | Terminal | Web |
| --- | --- | --- |
| Title | **Plugins (grid)**, **Plugins (editor)**, **Plugins (sidebar)**, or **Plugins (workspace)** elsewhere | **Plugins** |
| Which plugins | those whose `contexts` include the focused pane, and those without `contexts` | the same; with no pane focused (the context is empty) every plugin is listed |
| Each entry | the name and `sql · python3`, `text · python3` or `js · alt+P` (output or `js`, the key, the command) | the name, the output (`sql`, `text`), a `js` chip, the key |
| Run | {{< key "Enter" >}} | click; the entry says `running…` |
| Nothing fits | `no plugins for this pane in ~/.config/webtuidb/plugins` | `No plugins for this place. Add a .toml file to the plugins folder in the config directory.` |

A file that does not parse, or a command plugin without `name` or `command`, is left out of both lists without a
message. The folder is read again each time you open the list or run a plugin, so a new or edited file is picked up
at once. Shortcuts are read only at start.

## Contexts

`contexts` lists the panes a plugin applies to; leave it out to apply everywhere.

| Context | Terminal: focus on | Web: focus in |
| --- | --- | --- |
| `editor` | an SQL editor (the assistant's input counts as one) | an SQL editor |
| `grid` | a table's data grid or a result grid | a data grid or a result grid |
| `sidebar` | the sidebar | the sidebar |
| (empty) | anything else | anything else |

In the terminal a shortcut honours `contexts` too: `plugin row-count does not apply in this pane (grid, sidebar)`.
The browser runs a shortcut wherever you press it.

## What a plugin receives

Every plugin gets the same context, as JSON on standard input (command plugins) or as the `context` object
(JavaScript):

{{< ui tui="automation-plugins-context" web="automation-plugins-context" >}}`show-context` run on the `orders` grid: the terminal on "Shop (MySQL)" (no schema), the browser on "Shop (Postgres)".{{< /ui >}}

```json
{
  "connection": "Shop (Postgres)",
  "driver": "postgres",
  "database": "shop",
  "context": "grid",
  "object": { "Schema": "public", "Name": "orders", "Kind": "table" },
  "selection": "SELECT 1",
  "cell": "2024-01-01 14:13:00+00"
}
```

| Field | Value |
| --- | --- |
| `connection`, `driver`, `database` | the workspace's connection name, driver and current database |
| `context` | `editor`, `grid`, `sidebar` or empty |
| `object` | the table: in the terminal the object under the sidebar cursor or of the current tab; in the browser the table of the current tab. `Schema` is empty on engines without schemas. Left out when there is none |
| `selection` | the selected text: the SQL editor's selection in the terminal, the page's text selection in the browser. Left out when empty |
| `cell` | the value under the grid cursor, as text (`NULL` for a null), when a grid has the focus. Left out otherwise |

## Command plugins (`*.toml`)

```toml
# ~/.config/webtuidb/plugins/select-star.toml
name = "select-star"                  # required: shown in the list, used in plugin:<name>
command = "python3"                   # required: the program (looked up in PATH)
args = ["-c", """
import json, sys
c = json.load(sys.stdin)
o = c.get("object") or {}
if not o.get("Name"):
    sys.exit("select-star: no table here")
name = (o["Schema"] + "." if o.get("Schema") else "") + o["Name"]
print(f"SELECT *\\nFROM {name}\\nLIMIT 100;")
"""]
contexts = ["sidebar", "grid"]        # optional: where it applies
output = "sql"                        # optional: text (default) | sql
# key = "alt+S"                       # optional: a shortcut (after the prefix)
```

| Key | Meaning |
| --- | --- |
| `name` | the plugin's name (required) |
| `command` | the program to run (required). It runs directly, not through a shell: use `command = "sh"`, `args = ["-c", "…"]` for pipes |
| `args` | its arguments, as a list |
| `contexts` | `editor`, `grid`, `sidebar`; empty = everywhere |
| `output` | `text`: show what it prints in a window. `sql`: put it in the SQL editor |
| `key` | a shortcut in the key map's spelling (`alt+S`, `F7`, `x`), pressed after the prefix |

The program inherits webtuidb's environment and working directory, gets the context JSON on standard input, and
its standard output is the result (the first 1 MiB is kept). When it exits with an error, the message is
`<name>: exit status 1: <its standard error>`, for example `select-star: exit status 1: select-star: no table here`.
It is stopped after 60 seconds in the terminal and after `[general] query_timeout_secs` in the browser.

{{< ui tui="automation-plugins-sql" web="automation-plugins-sql" >}}`select-star` (output `sql`) run from the `orders` grid of "Shop (MySQL)": no editor had the focus, so the SELECT opens in a new SQL editor tab.{{< /ui >}}

What happens with the output:

| | Terminal | Web |
| --- | --- | --- |
| `text` | a window titled with the plugin's name; {{< key "c" >}} copies, {{< key "Esc" >}} / {{< key "q" >}} closes | the Plugins dialog shows it, with **Back** and **Close**; from a shortcut it opens in the details window |
| `sql` | typed at the cursor of the SQL editor when one has the focus (`select-star: inserted into the editor`), else a new SQL editor tab | inserted into the SQL editor that has the focus, else a new query tab |
| Nothing printed | `select-star printed nothing` | the dialog closes |

## JavaScript plugins (`*.js`)

A `.js` file runs in an embedded JavaScript engine (goja, ES5.1 with most of ES2015+), not in Node: there is no
`require`, file system or network. Its settings are `//` comments at the top of the file:

```js
// name: row-count
// key: alt+P
// contexts: grid, sidebar
// JavaScript plugin: counts the rows of the current table in a read-only transaction,
// copies the number and shows a notification.
const o = context.object
if (!o || !o.Name) throw new Error("open or select a table first")
const name = (o.Schema ? o.Schema + "." : "") + o.Name
const r = query("SELECT count(*) AS n FROM " + name)
const n = r.rows[0][0]
copy(n)
notify(name + ": " + n + " rows (copied)")
"Rows in " + name + ": " + n + "\nColumns returned: " + r.columns.join(", ")
```

{{< ui tui="automation-plugins-js" web="automation-plugins-js" >}}`row-count` on the `orders` table: the text in a window, the notification `orders: 600 rows (copied)` and the count on the clipboard.{{< /ui >}}

| Header comment | Meaning |
| --- | --- |
| `// name: …` | the plugin's name; default the file name without `.js` |
| `// key: …` | a shortcut, as for command plugins |
| `// contexts: a, b` | comma separated contexts |
| `// output: sql` | the final string goes to the SQL editor instead of a window |

The header ends at the first line that is neither blank nor a `//` comment. The script's last expression, when it
is a string, is the plugin's text output.

### The API

| Name | Does |
| --- | --- |
| `context` | the context object above (`context.object.Name`, `context.cell` …) |
| `query(sql)` | runs one read-only statement on the workspace's connection and returns `{columns: [...], rows: [[...], ...]}`, every value as a string |
| `copy(text)` | copies the text to the clipboard |
| `open(schema, table)` | opens that table in a new tab (`open("", "orders")` uses the current schema) |
| `notify(text)` | shows the text as a notification |
| `sql(text)` | opens the text in a new SQL editor tab (from the browser's Plugins dialog: inserted into the focused editor) |
| `throw new Error("…")` | stops the plugin; the error is shown, prefixed with the plugin's name, and no effect runs |

`copy`, `open`, `notify` and `sql` are effects: they are collected while the script runs and carried out, in order,
after it ends. The terminal shows the text first and then applies the effects; the browser applies the effects
first. For example, a plugin that writes a query for the cell under the cursor:

```js
// name: orders-of-customer
// contexts: grid
if (!context.cell) throw new Error("put the cursor on a customer id")
sql("SELECT * FROM orders WHERE customer_id = " + Number(context.cell) + " ORDER BY placed_at DESC;")
open("", "customers")
```

### Limits of JavaScript plugins

| Limit | Value, and the message |
| --- | --- |
| Script size | 1 MiB (`script larger than 1048576 bytes`) |
| Run time | 10 seconds (`row-count: context deadline exceeded`) |
| Memory | 256 MiB of heap growth (`row-count: stopped, it used more than 256 MiB of memory`) |
| Effects | 100 per run (`too many effects (at most 100)`) |
| Text | 1 MiB for the output and for each effect's text |
| `query()` rows | 1000 in the terminal, `[general] row_limit` in the browser |
| `query()` statements | one read-only statement. Refusals: `plugins may only run read-only statements` (terminal), `query(): only a single read-only statement is allowed` (browser), and the side-effect functions of the [MCP read-only check](../mcp-server/#how-execute_read_query-stays-read-only): `nextval is not allowed in a plugin query` |

`query()` runs inside a read-only transaction, like the MCP server's `execute_read_query`, so it works on
PostgreSQL, MySQL and MariaDB, and refuses engines without one
(`read-only queries cannot be enforced read-only on this redis connection …`). A null value is `NULL` in the
terminal and an empty string in the browser.

## Shortcuts and conflicts

`key = "alt+P"` (or `// key: alt+P`) binds the action `plugin:<name>` in the global context: press the prefix, then
the key ({{< key "C-d M-P" >}}). A plugin key cannot be a no-prefix binding. Change it without editing the plugin
in `[keys]`:

```toml
[keys]
"plugin:row-count" = "M-r"
```

The help screens ({{< key "C-d ?" >}} and the web help) list plugin shortcuts as `plugin: row-count`. A plugin key
that clashes with a built-in binding, a `[keys]` entry or another plugin, or that does not parse, is skipped; the
other plugins keep theirs:

| Where | Message |
| --- | --- |
| Terminal | a status line that stays until the next message: `plugin shortcut skipped: plugin:x (alt+p): …` |
| Web | a notification once per page: `Plugin shortcut conflicts: plugin:x (alt+p): …` |
| `webtuidb -keys` | `webtuidb: plugin shortcut skipped: …` on standard error |

A `[keys]` entry for a plugin that is not installed is ignored, so removing a plugin never breaks startup.

## Settings

The plugins folder is `~/.config/webtuidb/plugins/` (or `$XDG_CONFIG_HOME/webtuidb/plugins/`). Every `*.toml` and
`*.js` file in it is a plugin; subfolders are not read. The examples on this page are in the repository under
`docs/tools/env/config/webtuidb/plugins/`.

{{< callout type="warning" >}}
A command plugin runs with your user's rights. Only install plugin files you have read. The browser never sends a
command: it names a plugin, and the server runs the file from the plugins folder.
{{< /callout >}}
