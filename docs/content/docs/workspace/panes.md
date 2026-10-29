---
title: Panes and focus
description: Moving the keyboard between panes, splitting the main pane right or a SQL editor down, the row-detail and results panes.
weight: 40
---

Only one pane has the keys at a time: the sidebar, the tab's grid or editor, its results, the row-detail pane
or the console. The terminal draws that pane's border in the accent colour and names it in the status line
(`DATA`, `EDITOR`, `OBJECTS`…); the web shows the browser's focus ring on it. Shortcuts that belong to a pane
only work while it has the keys.

{{< ui tui="workspace-split-right" web="workspace-split-right" >}}`customers` and `orders` side by side after Split pane right.{{< /ui >}}

## Moving the focus

| Action | Keys | Terminal | Web |
|---|---|---|---|
| `focus_left` | {{< key "C-d M-h" >}}, {{< key "C-d C-h" >}} | From the row-detail pane back to the grid; otherwise to the sidebar (when it is shown). | The nearest visible area to the left. |
| `focus_right` | {{< key "C-d M-l" >}}, {{< key "C-d C-l" >}} | From the sidebar to the tab; from a data grid into the row-detail pane, when it is shown. | The nearest area to the right. |
| `focus_down` | {{< key "C-d M-j" >}}, {{< key "C-d C-j" >}} | From a SQL editor to its results (when there are results and they are shown); from anywhere else to the console (when it is shown). | The nearest area below. |
| `focus_up` | {{< key "C-d M-k" >}}, {{< key "C-d C-k" >}} | From the console back to where you were; from results to the editor. | The nearest area above. |
| `next_pane`, `prev_pane` | {{< key "C-d M-]" >}}, {{< key "C-d M-[" >}} | In a SQL editor split down: the other editor. Otherwise, with the main pane split: the other pane. | The same. |

`focus_left`, `focus_right` and `focus_up` also take the keys out of the console. {{< key "Esc" >}} leaves the
row-detail pane (back to the grid) and the console (after clearing a console search). In the web, clicking an
area gives it the keys; the shortcuts look for the nearest area marked as a pane in that direction, so they
work the same whatever is open.

## Split pane right

`split_right` ({{< key `C-d "` >}}) puts two tab groups side by side. Each group has its own tab bar and its
own active tab; new tabs open in the group that has the focus, and `next_tab`, `prev_tab` and
{{< key "C-d 1" >}}… cycle inside it.

| | Terminal | Web |
|---|---|---|
| First split | Needs two tabs (`open a second tab to split the pane`); the active tab moves to the right group, which takes the focus. | The active tab moves to the right pane, which takes the focus; with a single tab the left pane is left empty. |
| Pressing it again | Moves the active tab to the other group. | With an empty right pane: closes the split. On a tab already on the right: `This tab is already in the right pane`. |
| Switch group | {{< key "C-d M-]" >}} / {{< key "C-d M-[" >}}, or pick a tab of the other group with `next_tab`… | The same keys, or click in the other pane. The focused pane's tab bar has an accent line on top. |
| Move a tab across | `split_right` again | Drag the tab onto a tab of the other pane. |
| Empty group | Closing the last tab of a group closes the split. | The pane says `This pane is empty. Open a tab here, or close the split.` |

`close_pane` ({{< key "C-d M-q" >}}) closes the focused group without closing any tab: its tabs join the other
group, whose active tab stays active. With no split: `there is one pane: nothing to close` (web: `No split pane
to close`). {{< key "C-d M-w" >}} was already taken by Close other tabs, hence `M-q`.

{{< callout type="info" >}}
In the terminal, mouse clicks in the main area are ignored while it is split; use the keys. The split is not kept
when you switch database (all tabs close), and it is not restored on the next connect. In the web the split is
part of what a page reload restores. On a phone-sized window the two panes stack vertically.
{{< /callout >}}

## Split a SQL editor down

`split_down` ({{< key `C-d M-"` >}}) stacks a second, empty editor under the first one in the same SQL tab, so you
can keep two drafts against the same session. The new editor takes the focus; {{< key "C-d M-]" >}} /
{{< key "C-d M-[" >}} (or a click, in both UIs) moves between them. Running sends the text of the focused editor.

{{< ui tui="workspace-split-down" web="workspace-split-down" >}}One SQL tab with two editors stacked.{{< /ui >}}

Pressing `split_down` again closes the editor that does not have the focus. The terminal asks first when that
editor has text (`Close the other editor pane?` *Its text is dropped.*); the web closes it at once.

{{< callout type="warning" >}}
The second editor is not saved with the tab: its text is not restored by `auto_save_tabs` or by a page reload,
and in the terminal a tab opened from a saved query stops saving back to that query once it is split, so the
new pane cannot overwrite it. A tab opened from a `.sql` file compares only the focused editor with the file
when it decides whether there are unsaved changes.
{{< /callout >}}

## The row-detail pane

`toggle_row_detail` ({{< key "C-d Space" >}}) shows the row under the grid cursor as a list of fields, on the
right of the workspace in the terminal (a quarter of the width, 30 to 46 cells) and beside the grid in the web.
It follows the cursor. In the terminal the line on top describes the current column (`integer · not null ·
no default · primary key`), `*` marks a field with a pending edit, and {{< key "C-d M-l" >}} gives the pane the
keys: {{< key "j" >}} / {{< key "k" >}} move between fields, {{< key "Enter" >}} edits one, {{< key "Esc" >}}
goes back to the grid. On a table's Structure view the web opens the column form of the selected column instead.

{{< ui tui="workspace-row-detail" web="workspace-row-detail" >}}The row-detail pane on `orders`.{{< /ui >}}

Editing in the row-detail pane is covered in [Browsing data](../../browsing-data/) and
[Editing data](../../editing-data/).

## The results pane

A SQL editor tab has its results under the editor (in the terminal about 45 % of the tab's height; with too
little room the results are not drawn). `toggle_results` ({{< key "C-d `" >}}) hides and shows them. Move into
the results with {{< key "C-d M-j" >}} and back with {{< key "C-d M-k" >}} or {{< key "Esc" >}}; while they
have the keys, the result shortcuts (copy, find, chart…) apply. See [Results](../../sql-editor/results/).

## What a tab keeps

| | Terminal | Web |
|---|---|---|
| Table tab: filters, sort, hidden columns, column widths | Per table and database, in `state.json` | The same store |
| SQL editor text and tab name | With `auto_save_tabs`, on the next connect | Page reload; `auto_save_tabs` on the next connect |
| Split right | Not kept | Page reload |
| Second editor (split down) | Not kept | Not kept |
| Row detail and results shown or hidden | Not kept | Not kept |

See [Tabs that come back](../tabs/#tabs-that-come-back).
