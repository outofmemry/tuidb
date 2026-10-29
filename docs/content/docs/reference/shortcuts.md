---
title: Shortcuts
description: Every default key binding, in the terminal and in the browser, grouped by the pane it applies to.
weight: 10
---

Every action is the prefix, then a key. The default prefix is {{< key "C-d" >}}; after it, a panel lists the keys that are valid in the focused pane, and {{< key "Esc" >}} cancels. A binding marked *no prefix* works on its own, like a tmux `bind -n` key. The web UI runs the same state machine, so the same sequences work in the browser; the few keys a browser reserves are listed at the end.

This table is generated from the running binary (`GET /api/meta`, the same source as `webtuidb -keys` and the two help screens), so it is the resolved default keymap. Your own `[keys]` overrides replace the defaults shown here.

Key notation: `C-` is Control, `M-` is Alt (Option on a Mac, which may need "Use Option as Meta" in your terminal), `S-` is Shift. Letters are case-sensitive: `s` and `S` are different bindings.

## Lookup order

The focused pane's context is searched first, then the global list. A key bound in the grid never shadows a global key elsewhere, and a user binding never loses to a context default.

## All bindings

{{< keymap-table >}}

## Keys the browser keeps

The web UI listens to every key the page receives, so the same sequences work in the browser. A few keys never reach a page, or mean something else there:

| Key | What happens in the browser | What to do |
|---|---|---|
| {{< key "C-d C-d" >}} (prefix twice) | the second press is handed to the browser, which bookmarks the page in most browsers | not needed in the web: text fields receive plain typing without the prefix |
| Ctrl+W, Ctrl+T, Ctrl+N, Ctrl+Tab | the browser closes or opens tabs and windows before the page sees the key | no default binding uses them; do not bind them under `[keys]` if you use the web |
| Cmd on a Mac | left to the browser, so its own shortcuts keep working | webtuidb never binds Cmd |
| Option (`M-`) on a Mac | the web matches the physical key, so {{< key "C-d M-f" >}} works even though Option types a special character | nothing; in the terminal, see [Troubleshooting](../troubleshooting/#alt--option-keys-do-nothing-on-macos) |
| {{< key "C-d C-Space" >}} (`complete`) | macOS uses Ctrl+Space to switch input sources when more than one is enabled | turn that system shortcut off, or rebind `complete` |

A no-prefix key whose action the web does not implement is left to the browser. Every keyed action is implemented in the web today; see [Actions](../actions/).

## See also

- [Actions](../actions/): every action name A to Z, with the page that documents it.

- [Key bindings](../../configuration/key-bindings/): rebinding, key name grammar, contexts.
- [Concepts](../../getting-started/concepts/): the prefix model explained.
