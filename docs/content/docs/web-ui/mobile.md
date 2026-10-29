---
title: On a phone
description: "How the web UI changes below 760 and 980 pixels wide: the object drawer, stacked rows, full-screen dialogs, the collapsed toolbar and what is hidden."
weight: 40
---

The web UI is built to be usable on a phone: the same pages, laid out for a narrow screen. Nothing needs a different URL or setting. The layout switches on the width of the browser window, so a narrow desktop window gets it too. There are two breakpoints:

| Width | What changes |
|---|---|
| under 980 px | The toolbar buttons other than **New query**, **Open anything** and **?** move into the **⋯** menu. |
| under 760 px | The phone layout, everything below. |

The screenshots on this page were taken at 390 x 844 pixels (the phone preset of the screenshot tool).

## The start page

{{< ui web="web-ui-mobile-start" >}}
The start page on a 390 px wide screen.
{{< /ui >}}

- The page has less padding and a smaller heading; the buttons in the header wrap onto a second line.
- Each connection row stacks: the colour bar, engine mark and name are on top, and the **⋯** and **Connect** buttons sit on their own line below.
- Dialogs (the connection form, **Export…**, **Import…**, **Settings**) fill the screen, with the labels above their fields instead of beside them, and the buttons at the bottom.

## The workspace

{{< ui web="web-ui-mobile-drawer" >}}
The workspace on a phone: no object list until you ask for it, the toolbar behind **⋯**.
{{< /ui >}}

**The top bar wraps to two lines.** The first line is the connection badge, with an **Objects** button to its left; the second holds the safe mode chip, the database picker and the **⋯** menu. The server version text is hidden. The **⋯** menu lists **New query**, **History**, **Saved queries**, **Open anything**, **Key bindings** and **Disconnect**.

{{< ui web="web-ui-mobile-menu" >}}
The top bar's **⋯** menu on a phone.
{{< /ui >}}

**The object list is a drawer.** **Objects** slides the sidebar in from the left over the page (up to 86 % of the width, at most 320 px) with a dimmed backdrop. Tap an object, or the backdrop, and it slides away. Everything the sidebar does on a wide screen works in the drawer; {{< key "C-d 0" >}} shows and hides it on a window that is wide enough to keep it open.

**The tab bar scrolls sideways** and keeps the tab you are on in view.

{{< ui web="web-ui-mobile-table" >}}
A table: the toolbar wraps onto three lines, the grid scrolls sideways.
{{< /ui >}}

**Tables.** The toolbar above the grid wraps. The grid scrolls in both directions with a sticky header and row numbers, so you can read a wide table by swiping. The hint "Double-click a cell to edit it" under the grid is hidden below 900 px to save room.

**Editors.** The run bar wraps, one group of buttons per line; the editor and the results stack as they do on a wide screen.

{{< ui web="web-ui-mobile-editor" >}}
A query editor on a phone, with the run bar wrapped.
{{< /ui >}}

**Panes stack.** A split pane puts the second pane below the first instead of beside it, each with its own tab bar. The row detail and the history and saved-query drawers cover the whole screen instead of sitting beside the grid. The filter bar wraps.

**The status bar** keeps the connection name, **details** and **Jobs**; the database and schema text is hidden.

## What a phone does not have

- **No hardware keyboard, no prefix keys.** Everything a shortcut does is reachable another way: the **⋯** menu, **Open anything** (which lists every action by name), the buttons in each tab, and the **⋯** menus on rows and objects. With a Bluetooth keyboard the prefix keys work as on a desktop.
- **No right-click.** Every menu that opens on a right-click also has a visible **⋯** button or a control for it. A long press opens the context menu on browsers that send a context-menu event for it.
- **No file drag and drop** from the desktop. Use **Import…** from a table's menu in the sidebar, or **Open file…**, and the phone's own file picker.
- **Reveal in folder** and the other actions that open the server's file manager only work on the machine running webtuidb. A phone is never that machine, so it downloads the file instead.

## Settings

The phone layout has no settings of its own. These settings still apply:

| Key | Meaning |
|---|---|
| `[web] font_size`, `[web] line_height` | Raise the font size if the text is small. |
| `[web] toolbar` | Which buttons the **⋯** menu replaces on a wide screen; the menu itself is fixed. |
| `[web] auto_hide_scrollbars` | Scrollbars appear only while scrolling, which suits touch screens. |

## See also

- [The browser app](../browser-app/) for the wide layout.
- [Serving the web UI](../serving/) for reaching the server from your phone (an SSH tunnel, TLS and a password are the safe way).
