---
title: Lock and settings
description: The passcode lock screen, and the in-app settings editor with its key-bindings editor, in both UIs.
weight: 80
---

Two things that are not about the data but live in every workspace: the lock screen that hides everything
behind a passcode, and the settings editor that changes `config.toml` without leaving the app.

## Passcode lock

With a passcode set, `lock` ({{< key "C-d M-L" >}}) covers the whole app with a lock screen until the passcode is
typed. Nothing of the workspace shows behind it, no shortcut reaches it, and no dialog can open over it. Your
tabs, pending edits and running queries stay as they are.

{{< ui tui="workspace-lock-none" web="workspace-lock-none" >}}`lock` without a passcode set: both UIs say how to set one.{{< /ui >}}

| | Terminal | Web |
|---|---|---|
| Lock now | {{< key "C-d M-L" >}}, or **Lock** in Open Anything | {{< key "C-d M-L" >}} |
| Locked at start | yes, when a passcode is set | yes, when a passcode is set |
| Idle lock | after `lock_after_mins` minutes without a key press or mouse click (checked every 15 seconds, counted from the first input) | after `lock_after_mins` minutes without mouse movement, a click, a key, the wheel or a touch |
| Lock screen | a box, **🔒 Locked**, *webtuidb is locked.*, a hidden `passcode` field; {{< key "Enter" >}} unlocks | **Locked**, a *Passcode* field and **Unlock** |
| Wrong passcode | `wrong passcode (1 of 3 tries)`; after three, `wrong passcode: wait 10 s` and keys are refused until then | `Wrong passcode`; after three wrong tries from one address, each further try waits one second |
| Way out without it | {{< key "C-d q" >}} quits; with uncommitted changes the first press says `uncommitted changes will be lost: press C-d q again to quit` | close the browser tab |
| No passcode set | `no passcode is set: run webtuidb -set-passcode and add the line it prints to config.toml` | `No passcode is set: run webtuidb -set-passcode and add the [security] line` |

Set the passcode with `webtuidb -set-passcode`: it reads the passcode twice and prints the `[security]
passcode = "…"` line (a bcrypt hash) to paste into `config.toml`. The passcode itself is never shown in the
settings editor; `lock_after_mins` is.

```toml
[security]
passcode = "$2a$10$…"   # from webtuidb -set-passcode
lock_after_mins = 15    # 0 = only when you lock it
```

{{< callout type="info" >}}
In the web the lock hides the page; the access token stays in the browser tab's storage, so it keeps a bystander
out of the page, not out of the API. The lock, the web token and safe mode are covered in [Safety](../../safety/).
{{< /callout >}}

## Settings editor

`settings` ({{< key "C-d ," >}}, or **Settings** in Open Anything) opens a form over `config.toml`: every setting
of the sections `[general]`, `[editor]`, `[table]`, `[csv]`, `[sidebar]`, `[theme]`, `[export]`, `[web]`,
`[queries]` and `[security]`, with its current value and a one-line description, plus an editor for the key
bindings. `[ai]`, `[mcp]`, `[[tag]]`, `[[source]]`, `[theme.colors]` and the secrets (`web.password`,
`security.passcode`) are edited in the file.

{{< ui tui="workspace-settings" web="workspace-settings" >}}The settings editor.{{< /ui >}}

### In the terminal

The dialog, **Settings**, shows the path of the file on top and the settings grouped by section. The setting
under the cursor has its description below the list.

| Key | Action |
|---|---|
| ↑ ↓, {{< key "j" >}} {{< key "k" >}}, {{< key "C-n" >}} {{< key "C-p" >}}, {{< key "PgUp" >}} {{< key "PgDn" >}} | Move |
| {{< key "Enter" >}}, {{< key "Space" >}} | Change: a true/false setting flips, a setting with fixed values opens a picker (the current one marked), anything else opens a small form with its description (`a whole number` for numbers, `comma-separated` for lists; `theme.name: "x" is not a whole number`-style errors stay in the form) |
| {{< key "s" >}}, {{< key "C-s" >}} | Save the changes |
| {{< key "r" >}} | Revert the change to the setting under the cursor |
| {{< key "/" >}} | Find: filter by name and description |
| {{< key "Tab" >}} | Switch between Settings and Key bindings |
| {{< key "e" >}} | Open `config.toml` in `$VISUAL` or `$EDITOR` (`vi` when neither is set) instead |
| {{< key "Esc" >}}, {{< key "q" >}} | Close (asks `Discard 2 unsaved changes?` when there are some) |

A changed setting has `*` before it and the title counts them (`Settings · 2 unsaved`). Saving validates every
value first; a refusal is shown in the dialog and nothing is written. When the file has comments, saving asks
first: `Saving drops the comments in config.toml` (*The form rewrites the file and does not keep comments. Press
n, then e to edit the file by hand in $EDITOR instead: that keeps them.*) with **Save anyway**. After a save the
file is read again and the dialog says `saved 2 settings. Theme, keys and most options are live; editor options
apply to new tabs; restart to apply the MCP and web settings.`

**Key bindings** lists every binding grouped by context: the key, the action name and its description, with `●`
after the ones you changed. {{< key "Enter" >}} opens **Key for commit**: *Key* (`alt+x`, `ctrl+g`; empty
restores the default), pressed after the prefix unless it is a no-prefix binding. The new keymap is built first,
so a key that is already taken is refused in the form with the conflict; otherwise the `[keys]` table is written
and the keys work at once (`commit rebound.`).

{{< ui tui="workspace-settings-keys" web="workspace-settings-keys" >}}The key-bindings editor.{{< /ui >}}

### In the web

The **Settings** dialog has the sections on the left (and **Keymap** at the end) and the selected section's
settings on the right: a checkbox for true/false, a list for fixed values, a number box, or a text box (lists
comma separated), each with its description. **Save N** writes the changed ones; an invalid value is shown under
its field and nothing is written. **Copy config path** copies the file's path.

- The settings that name files or folders on the server (`queries.shared_dirs`, `theme.file`, `web.tls_cert`,
  `web.tls_key`) are shown read only: *Set in the config file; the web UI does not write paths.*
- Saving does not ask about comments; afterwards it says `comments in config.toml were not kept` when there
  were some.
- A change to `[theme]` applies to the page at once; for anything else the dialog says `Saved. Restart webtuidb
  for these to apply.`

**Keymap** lists every action with its description, its name and one box holding all its keys, separated by
spaces, in the `[keys]` spelling (`alt+q`, `ctrl+d`, `P`, and `-n` before a key that needs no prefix).
{{< key "Enter" >}} or **Save** writes that action's keys; a key that is taken is refused with the conflict
beside the action, and an empty box says `Give the action at least one key.` (the web cannot restore a default by
clearing the box; edit the file or use the terminal for that).

### Details

Both editors rewrite `config.toml` from the parsed values: comments and the original layout of the file are lost
(edit by hand to keep them). The values are checked the same way as at start-up, so the editor cannot write a
file that would not load. The full list of settings is in the
[settings reference](../../configuration/settings-reference/), and `[keys]` in
[Key bindings](../../configuration/key-bindings/).
