---
title: Concepts
description: The prefix key, the help screen, Open Anything, pending changes and safe mode, and what the terminal and web UIs share.
weight: 30
---

## The prefix, then a key

webtuidb is driven like tmux: press the prefix, {{< key "C-d" >}} by default, then one key. After the prefix a panel lists every key that is valid where you are, and {{< key "Esc" >}} cancels. {{< key "C-d C-d" >}} sends a literal Ctrl+d to the focused widget.

{{< ui tui="workspace-prefix" web="workspace-prefix" >}}The panel that opens after the prefix, in both UIs.{{< /ui >}}

Typing, arrows, {{< key "j" >}}/{{< key "k" >}} and {{< key "Enter" >}} inside a widget are never prefixed, and the plain keys of a pane (`/` to find, `s` to sort, `e` to edit, `n` for a new connection) are fixed. Moving between panes is {{< key "C-h" >}} {{< key "C-j" >}} {{< key "C-k" >}} {{< key "C-l" >}} without the prefix.

Every prefixed action has a name (`commit`, `filters`, `open_anything`) and can be rebound in `config.toml`; the prefix itself is `[general] prefix`. The web UI reads the resolved keymap from the server, so the same sequences work in the browser, and the few chords a browser keeps for itself are listed in [Shortcuts](../../reference/shortcuts/).

## The help screen

{{< key "C-d ?" >}} lists the bindings for the pane you are in. In that list {{< key "/" >}} fuzzy-searches by name, description or key, and {{< key "Enter" >}} runs the selected command. The web help dialog shows the same list in the same order; actions the browser cannot perform are greyed out with the reason.

{{< ui tui="reference-dialog-help" web="reference-dialog-help" >}}The help screen.{{< /ui >}}

## Open Anything

{{< key "C-d p" >}} opens a fuzzy finder over tables, saved queries, history, connections and actions. It is the fastest way to jump to a table in a large schema. Details are in [Open Anything](../../workspace/open-anything/).

{{< ui tui="workspace-open-anything" web="workspace-open-anything" >}}Open Anything.{{< /ui >}}

## Pending changes

Editing a cell, adding or deleting a row never touches the database by itself. Each edit becomes a mark in the grid, and nothing is sent until you commit with {{< key "C-d s" >}}. {{< key "C-d P" >}} shows the exact statements first, {{< key "C-d Backspace" >}} discards everything, {{< key "C-d z" >}} and {{< key "C-d Z" >}} undo and redo. See [Editing data](../../editing-data/).

## Safe mode

Each connection has a safe mode: `silent`, `alert1`, `alert2`, `safe1` or `safe2`. Alert modes ask for confirmation and show the statements; Safe modes ask for the database password; mode 2 lets `SELECT`, `EXPLAIN` and `SHOW` through. Production connections usually run `alert2` or `safe2`. {{< key "C-d S" >}} changes the level for the session. See [Safe mode](../../safety/safe-mode/).

{{< ui tui="reference-dialog-safe-mode" web="reference-dialog-safe-mode" >}}A safe-mode prompt.{{< /ui >}}

## One engine, two front ends

The terminal UI and the web UI are the same program. They share `connections.toml`, `config.toml`, query history, saved queries, pinned and recent tables and per-table filters and sort. The web server enforces each connection's safe mode on the server side and exports the keymap and menus through `GET /api/meta`, which is how the two help screens stay identical. The project tests this parity in CI (`make parity`).
