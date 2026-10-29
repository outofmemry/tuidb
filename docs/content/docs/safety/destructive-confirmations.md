---
title: Destructive confirmations
description: Typed-name confirmations for drop, truncate and delete, unsent changes on close and disconnect, restore into a new database, read-only connections and bootstrap statements, in the terminal and in the browser.
weight: 20
---

Some actions cannot be undone: dropping a table, truncating it, dropping a schema or a database, throwing
away edits you have not sent. webtuidb makes you type the name of the thing (or a count) before any of them,
and in the terminal a reflexive {{< key "Enter" >}} never says yes to them. These prompts come before
[safe mode](../safe-mode/): you type the name first, then the planned statements still pass the safe mode
check of the connection.

{{< ui tui="safety-typed-name" web="safety-typed-name" >}}Deleting `customers`: the name is typed only partly, so the terminal shows "type “customers” to confirm" and the web OK button would refuse it.{{< /ui >}}

## Typed-name confirmations

| What | Terminal | Web |
|---|---|---|
| Delete one table, view or other object (`delete_item`, {{< key "C-d Delete" >}} in the sidebar) | form "Delete *name*?" with **Cascade**, the line "This cannot be undone. Type the name to confirm." and the field **Type its name**, which has the focus; the footer says "↵ delete" | prompt "Delete *name*? This cannot be undone. Type its name to confirm" |
| Truncate one table (`truncate`, {{< key "C-d M-Delete" >}}) | the same form titled "Truncate *name*?"; on PostgreSQL it adds **Restart identity** | the same prompt with "Truncate" |
| Delete or truncate several marked tables | form "Delete 3 tables?" listing the names, **Cascade**, and "Type 3 (the number of tables) to confirm." | no typed confirmation: the statements go straight to the "Send N statements?" preview |
| Drop a schema | picker "Drop which schema?", then form "Drop schema *name*?" with **Cascade (drops what is in it)** and **Type its name** | prompt "Type the name of the schema to drop (*current* is the one shown)", then "Type *name* again to drop schema *name* and everything in it", then a choice **Drop with its contents** / **Drop only if empty** |
| Drop a database (database list, `drop`) | form "Drop *name*?" with "This deletes the database and everything in it." and **Type its name** | prompt "Type *name* to drop it" |
| Drop a user or role | "This cannot be undone. Type the name to confirm." | see [Users and roles](../../tools/users/) |
| Delete a connection group with its connections | **Type its name**; refused while one of them is still sending a change | prompt "Type *group* to delete this group and its N connections"; a mismatch says "The name did not match; nothing deleted."; refused when the group holds connections from a `[[source]]` file |
| Delete a metrics board | form "Delete board *name*?", "This removes *path*.", **Type its name** | see [Metrics boards](../../tools/metrics-boards/) |
| Apply a data diff, bulk update or fake rows batch | when the batch has a `DELETE`, `DROP` or `TRUNCATE`, or more than 100 statements: form "Apply N statements?" with "Type N to confirm" | the server's "Send N statements?" preview |

What happens when the text does not match:

- Terminal: the form stays open with a red "✗ type “*name*” to confirm" (or "type 3 to confirm") and nothing
  is planned. The comparison is exact and case-sensitive. {{< key "Esc" >}} cancels.
- Web: the prompt closes and a notice says `Type "name" to confirm; nothing changed.` (or "The names did not
  match; nothing dropped." for schemas and databases). Nothing is sent.

After a matching name, the web asks two more questions for an object on a SQL engine: "Also drop everything that
depends on *name*?" (**Drop with dependents** / **Drop only this**) and, for a PostgreSQL truncate, "Also reset
the sequences of the table (restart identity)?" (**Reset the sequences** / **Keep the sequences**). The terminal
has the same choices as the Cascade and Restart identity checkboxes in its form. Either way the statements then
appear in the preview and pass the safe mode gate.

The open database cannot be dropped in the terminal: "the open database cannot be dropped; open another one
first".

## Enter never confirms in the terminal

Confirmation dialogs that lose something (red frame and a ⚠ in the title) answer only to `y`:

| Key | Ordinary dialog | Destructive dialog |
|---|---|---|
| `y` | yes | yes |
| {{< key "Enter" >}} | yes | **cancel** |
| {{< key "Esc" >}}, `n`, `q` | cancel | cancel |
| `j` / `k`, arrows | scroll a long text | scroll a long text |

The buttons show it: "Disconnect  y" and "Cancel  ↵/Esc/n". The MCP approval sheet additionally ignores every
key for the first 700 ms after it opens.

{{< callout type="warning" >}}
The browser does not have this rule. A web confirmation puts the focus on its primary button (**Discard**,
**Delete**, **Run**…), so {{< key "Enter" >}} presses it. Only {{< key "Esc" >}}, ✕, **Cancel** or a click
outside the dialog cancel. Typed-name prompts are not affected: Enter submits what you typed, and a wrong name
changes nothing.
{{< /callout >}}

## Closing and disconnecting with unsent changes

{{< ui tui="safety-unsent-disconnect" web="safety-unsent-disconnect" >}}A row of `orders` is marked for deletion but not sent; disconnecting asks first.{{< /ui >}}

| Situation | Terminal | Web |
|---|---|---|
| Close a tab with pending edits | "Close *tab* with unsaved changes?" / "Pending edits in this tab will be lost." (**Close**, destructive) | "Discard pending changes in the tab(s) being closed?" (**Discard**) |
| Close a query tab with an open transaction | "This tab has an open transaction. Close it and roll back?" (**Roll back and close**) | the same text and button |
| Close a modified SQL file tab | "Save changes to *file*?" with Save / Don't save / Cancel | — |
| Close several tabs | "Close N tabs with unsaved changes?" | the same "Discard pending changes…" question |
| Disconnect (`disconnect`, {{< key "C-d M-." >}}) | always asks "Disconnect *name*?" / "All of its tabs are closed."; destructive (only `y`) when a tab has unsent changes | asks only when a tab has unsent changes or an open transaction, with the two questions above |
| Close the other connections | "Close the other connections?"; with unsent changes "N of them have unsaved changes that will be lost." (destructive) | — |
| Switch database | "Switching database closes all tabs" / "There are unsaved changes in open tabs; they will be lost." (destructive) | the unsent-changes questions, then "Switching closes all open tabs. Continue?" |
| Reconnect | — | "Reconnect to *name*? Unsent changes in open tabs are lost." |
| Reload a table, change its filter or page | "Discard pending changes?" / "Reload and discard pending changes?" | — |
| Run a query again over edited results | "Discard pending result edits?" | — |
| Leave the structure view with unsent structure edits | — | "Discard the unsent structure changes?" |
| Quit (`quit`, {{< key "C-d q" >}}) | "Quit with uncommitted changes?" / "Pending edits will be lost."; "A commit is still running. Quit anyway?"; "*tab* has an open transaction. Quit and roll back?" | closing or reloading the browser tab triggers the browser's own "Leave site?" warning |

## Restore into a new database

A restore overwrites data, so the terminal treats it like a `DROP`: the restore command line (for example
the `pg_restore …` call) is the "statement" that passes the safe mode gate, and the "Restore *db*?" dialog is
destructive (only `y`). With **Restore into a new database** the dialog says "First creates database *name*:"
followed by the `CREATE DATABASE` statement, then the restore command. The create goes through the gate first;
the restore runs only if it succeeded.

If the restore then fails, the database it created is empty or half filled:

- Terminal: a destructive dialog "Drop the new database *name*?" / "The restore failed." with the `DROP DATABASE`
  statement. `y` drops it (through the gate); anything else keeps it.
- Web: the error ends with "(database *name* was created before the restore and is still there; drop it if
  you do not want it)".

Backup and restore are refused on a `read_only` connection in both UIs ("*name* is read-only: backup and
restore are refused" / "this connection is read only; backup and restore are refused"), because the dump tools
log in on their own and the session's read-only guard does not reach them.

## Password levels without a stored password

Safe Mode 1 and Safe Mode 2 ask for the password the connection was opened with. When that password is empty
(no `password`, or a `password_cmd` that printed nothing), a blank field would always match, so it is never
offered:

- Terminal: a destructive dialog "Safe Mode 2: this connection has no stored password. Send?" with the statements.
  Only `y` sends. Lowering the level away from Safe Mode 1/2 shows "This connection has no stored password.
  Change safe mode to *level*?".
- Web: the server turns the password gate into a confirmation ("confirm changing safe mode from Safe Mode 2" when
  changing the level).

## Read-only connections

`read_only = true` on a connection (the **Read only** checkbox in the connection form) means "this connection
must never write". The terminal shows **READ-ONLY** in the status bar and **RO** next to the name in the
connection list; the web shows a red **read only** chip in the top bar.

{{< ui tui="safety-read-only-refused" web="safety-read-only-refused" >}}An `UPDATE` on the read-only production replica. The terminal asked the Alert Mode 2 question first, then the driver refused; the browser refuses before asking.{{< /ui >}}

How it is enforced depends on the engine and the UI:

| Engine | Terminal | Web |
|---|---|---|
| PostgreSQL, Redshift, Greenplum, Vertica | the server: every connection starts with `default_transaction_read_only = on`, and the driver refuses statements the classifier calls writes ("connection is read_only") | the same, plus the web check below |
| MySQL, MariaDB, SingleStore | the server: every connection runs `SET SESSION TRANSACTION READ ONLY` first, plus the same driver check | the same, plus the web check below |
| ClickHouse, Cassandra, DynamoDB, Redis, MongoDB | **not enforced**: the statement goes through safe mode and is sent | the classifier: any statement it does not call read-only is refused with "this connection is read-only; data-changing statements are refused" (HTTP 403) |

{{< callout type="warning" >}}
In the terminal, `read_only` on a ClickHouse, Cassandra, DynamoDB, Redis or MongoDB connection is a label only:
writes from the SQL editor, table edits and structure changes are sent. Use a database account without write
permission, or the browser, or set `safe_mode = "alert1"` or `"safe1"` so that every statement asks first.
{{< /callout >}}

Elsewhere `read_only` also:

- turns off editing of table data and query results in the browser;
- makes plugin queries run on the read-only server session (see [Safe mode](../safe-mode/#where-safe-mode-applies));
- refuses backup and restore (above);
- is reported to MCP clients for the connection.

The classifier is the one described under [what counts as read-only](../safe-mode/#what-counts-as-read-only):
on the web it refuses anything it is not sure about, so a read-only connection may refuse an odd but harmless
statement.

## Bootstrap statements

A connection's `bootstrap` list (in `connections.toml`, or **Bootstrap** in the connection form) runs on
connect, without safe mode and without a prompt. They are meant for session settings such as
`SET search_path TO app` or `SET statement_timeout = '30s'`.

- PostgreSQL and MySQL run them on every new physical connection of the pool, so they survive reconnects. A
  failing statement fails that connection ("bootstrap (*statement*): *error*").
- The other engines run them once after connecting. A failure does not close the connection; the terminal shows
  "Bootstrap command failed (the connection is still open)" with each failed statement and its error, and the
  web shows the same title in a dialog with **OK**.
- On a `read_only` PostgreSQL or MySQL connection the server refuses a bootstrap statement that writes.
- Importing connections from a file drops `bootstrap` (see [Secrets and files](../secrets-and-files/#exporting-and-importing-connections)).

## Details and limits

- A batch of several marked tables on MySQL runs statement by statement; when one fails, the earlier ones stay
  applied and the sidebar reloads to show what is there now.
- The data tools' "Type N to confirm" counts `DELETE`, `DROP` and `TRUNCATE` by the first word of each statement.
- Web and terminal differ in two places: the web has no typed count for several marked tables, and Enter can
  confirm a web dialog.
