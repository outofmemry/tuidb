---
title: Pending changes
description: Edits, new rows and deletions are marks in the grid until you commit; how they are shown, kept and sent.
weight: 10
---

webtuidb never writes to the database while you type. An edited cell, a new row and a deleted row are *marks* held in the tab, and the database only changes when you commit ({{< key "C-d s" >}}).

{{< ui tui="editing-pending-marks" >}}An edited cell (amber, old value kept) and a deleted row (struck through) waiting to be committed.{{< /ui >}}

## How marks look

| Change | Terminal | Web |
|---|---|---|
| Edited cell | amber value, the original kept until commit | amber with an underline; the changeset tray lists old value struck through next to the new one |
| New row | green row number | green row number |
| Deleted row | struck through in coral | struck through in coral |

The terminal status line counts pending changes; the web shows the changeset tray with a **Send changes** button. Nothing is sent until you press it or {{< key "C-d s" >}}.

## What survives

Marks stay attached to their rows by primary key, so sorting, paging, filtering, hiding columns or switching the schema does not lose them. Closing the tab, disconnecting or switching the database asks first when marks are pending. One commit runs at a time per tab; while it is in flight the grid is read-only.

## Tables without a primary key

A table such as `no_pk` has nothing to address a row by. The terminal still lets you update or delete after a confirmation and matches the row by all of its column values; the web refuses with "this table has no primary key". Edits against a primary key you have hidden from view are still keyed correctly.

## How a commit is built

Each marked row becomes one statement in the connection's dialect: `UPDATE … WHERE pk = …` for edits, `INSERT` for new rows, `DELETE` for removed rows. MongoDB and Redis get the equivalent update documents and command sequences. {{< key "C-d P" >}} shows the exact statements before anything is sent, and the commit goes through the connection's [safe mode](../../safety/safe-mode/) and `read_only` setting like any other write. After a successful commit the page reloads so you see what the server kept (triggers and defaults included); on an error the marks stay so you can fix and retry.

Every committed statement is appended to the connection's query history with `src: "gui"`. With `[general] log_original_values = true` the history entry also carries a CSV pre-image of the key values, which is enough to write the reverse statement by hand.
