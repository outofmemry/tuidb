---
title: Commit, preview, discard
description: Review the statements a commit will run, send them, discard them, or undo and redo single edits.
weight: 40
---

| Action | Key | Name |
|---|---|---|
| Preview the SQL | {{< key "C-d P" >}} | `preview` |
| Commit | {{< key "C-d s" >}} | `commit` |
| Discard every pending change | {{< key "C-d Backspace" >}} | `discard` |
| Undo the last edit | {{< key "C-d z" >}} | `undo` |
| Redo | {{< key "C-d Z" >}} | `redo` (terminal; the web has undo only) |

**Preview** opens the statements in the connection's dialect, one per marked row, in the order they will run. For MongoDB it shows the update documents, for Redis the command sequence (wrapped in `MULTI` after the key types have been checked). Nothing is sent from the preview.

**Commit** runs the statements. On a connection in `alert1`/`alert2` the same statements are shown again for confirmation; in `safe1`/`safe2` the database password is asked. A `read_only` connection refuses the commit before anything is sent. See [Safe mode](../../safety/safe-mode/). When the commit succeeds the marks are cleared and the page is reloaded; a failing statement stops the commit and leaves the remaining marks in place with the server's error in the console and the message view.

**Discard** drops every mark in the tab at once, after a confirmation when there is more than one. **Undo** steps back through edits one at a time; the web keeps an undo stack per tab but no redo.

{{< callout type="warning" >}}
MySQL commits DDL implicitly, and on every engine each statement of a commit is sent on its own, so a commit that fails half-way can leave the earlier rows changed. Use the preview on anything you cannot easily reverse, or run the change as one statement from the SQL editor.
{{< /callout >}}
