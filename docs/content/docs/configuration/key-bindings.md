---
title: Key bindings
description: Rebind any action in config.toml or the keymap editor, the key name grammar, contexts and lookup order, no-prefix keys, and what browsers keep for themselves.
weight: 50
---

Every shortcut in webtuidb is an *action* with a key. Most keys are pressed after the prefix ({{< key "C-d" >}} by default), the way tmux works; a few work on their own. You can change the prefix, give any action a different key or several keys, and make a key work without the prefix. The terminal and the web read the same keymap, so a change applies to both UIs.

{{< ui tui="configuration-keymap" web="configuration-keymap" >}}The keymap editor: the **Key bindings** tab of the terminal settings dialog ({{< key "C-d ," >}}, then {{< key "Tab" >}}) and the **Keymap** section of the web settings dialog.{{< /ui >}}

The default key of every action is in [Shortcuts](../../reference/shortcuts/).

## How to rebind

Three ways, all writing the same `[keys]` table of `config.toml`:

| Where | How |
|---|---|
| `config.toml` | Add `action = "key"` under `[keys]`. Read at start. |
| Terminal | {{< key "C-d ," >}}, {{< key "Tab" >}} to **Key bindings**, pick the action, {{< key "Enter" >}}, type the key. Applies at once. |
| Web | {{< key "C-d ," >}} or **Settings** → **Keymap**, type in the action's field, {{< key "Enter" >}} or **Save**. |

```toml
[general]
prefix = "C-a"                    # the prefix key

[keys]
commit  = "x"                     # one key
run_all = ["M-Enter", "-n F5"]    # several keys; "-n " = no prefix needed
"plugin:hello" = "M-h"            # a plugin's shortcut
```

A key you give an action **replaces all of its default keys**, in every context the action has. To keep the default too, list it: `commit = ["s", "x"]`.

## Key names

Keys are written tmux-style. The parser also accepts the spelling the web and plugin files use (`ctrl+d`, `alt+P`, `ctrl+shift+x`).

| Write | Means |
|---|---|
| `a`, `P`, `]`, `?`, `#` | that character; letters are case-sensitive (`s` and `S` are different keys) |
| `C-x` | Control+x (`C-X` is the same key: Control folds the case) |
| `M-x` (or `A-x`) | Alt+x; Option on a Mac (your terminal may need "Use Option as Meta") |
| `S-x` | Shift+x; for a letter this is the capital: `S-p` is `P` |
| `C-M-x`, `M-S-Tab` | modifiers combine, in any order |
| `Enter` (`Return`), `Tab`, `Space` (or a literal space), `Esc` (`Escape`), `BSpace` (`Backspace`), `Delete` (`Del`), `Insert` | named keys, case-insensitive |
| `Up`, `Down`, `Left`, `Right`, `Home`, `End`, `PgUp` (`PageUp`), `PgDown` (`PageDown`) | navigation keys |
| `F1` … `F24` | function keys |
| `M-"`, `M-{`, `C-M-!` | Alt (or Ctrl+Alt) with a shifted punctuation or digit key: write the character Shift produces. `M-S-'` and `C-M-S-1` are read as `M-"` and `C-M-!` (US layout), the same as what the terminal reports. |

Unknown modifiers or names are errors: `bad key "C-Z-x": unknown modifier "Z"`, `bad key "hyper": unknown key name "hyper"`.

{{< callout type="info" >}}
Some keys cannot be bound because the terminal sends the same code for them: `C-[` is `Esc`, `C-i` is `Tab`. That is why `show_data` and `show_structure` use `(` and `)`, and `show_index` / `show_ddl` use `M-i` / `M-d`.
{{< /callout >}}

## Rules

- **`Esc` cannot be bound and cannot be the prefix**: it cancels a pending prefix.
- **The prefix key cannot be bound** to an action: `C-d is the prefix key and cannot be bound`. Pressing it twice sends it to the focused widget literally (like tmux).
- **Two of your bindings on one key** in the same context (and both prefixed or both not) is an error: `[keys] next_tab and prev_tab are both bound to M-q in global`.
- **Taking a default key**: in `config.toml` your binding wins and the action that had the key loses it; the start reports `[keys] commit = "x" takes the key from quick_filter (grid); quick_filter is now unbound`. A global binding frees the key in every context; a context binding frees it only in that context. The keymap editors refuse such an edit instead (below).

All `[keys]` errors and the startup report: [The config file → `[keys]` syntax](../config-file/#keys-syntax).

## Contexts and lookup order

Each binding belongs to a context: the pane it works in.

| Context | Where |
|---|---|
| `global` | everywhere |
| `sidebar` | the object list on the left |
| `grid` | a table's data grid |
| `filterbar` | the filter bar above the grid |
| `structure` | a table's structure view |
| `editor` | the SQL editor |
| `results` | the results pane under the editor |
| `console` | the console log pane |

After the prefix, the focused pane's context is searched first, then `global`. So the same key can mean different things in different panes: {{< key "C-d f" >}} is **find** in the editor and **show filters** in the grid. An action can exist in several contexts (`find`, `add_row`, `copy_rows` …); rebinding it changes its key in all of them.

In the web, the focused context comes from the element that has focus (or was clicked last): grid, editor, sidebar and the other panes each mark their area, and anything else is `global`.

## No-prefix bindings

A key written with `-n ` in front works without the prefix, like tmux `bind -n`. The defaults that work this way:

| Keys | Context | Actions |
|---|---|---|
| {{< key "C-h" >}} {{< key "C-j" >}} {{< key "C-k" >}} {{< key "C-l" >}} | global | `focus_left`, `focus_down`, `focus_up`, `focus_right` (these take the editor's own `C-h` and `C-k`; rebind them to get those back) |
| {{< key "M-c" >}} {{< key "M-e" >}} {{< key "M-n" >}} | filterbar | `filter_clear_all`, `filter_export`, `filter_add_copy` |
| {{< key "/" >}} {{< key "n" >}} {{< key "N" >}} {{< key "y" >}} {{< key "e" >}} {{< key "m" >}} | console | `console_find`, `console_next`, `console_prev`, `console_copy`, `console_errors`, `console_menu` |

```toml
[keys]
run_all = ["M-Enter", "-n F5"]   # F5 runs everything without the prefix; C-d M-Enter still works
```

A no-prefix key takes the key away from the focused widget: bind `-n` only to keys you never type as text. In the web, a single printable character never fires a no-prefix binding while a text field has focus; it is typed instead.

## `webtuidb -keys`

```sh
webtuidb -keys
```

prints the resolved keymap (defaults, your `[keys]`, and installed plugins' shortcuts) and exits:

```text
prefix: C-d   (every action below is: prefix, then the key; "-n" bindings need no prefix)

[global]
  help                       ?          list key bindings
  open_anything              p          open anything
  ...
[editor]
  run_all                    M-Enter    run all statements
  run_all                    -n F5      run all statements
```

Groups follow the context order above; inside a group, the order is the keymap table's, the same order both help screens ({{< key "C-d ?" >}}) use. A `[keys]` warning and skipped plugin shortcuts go to stderr first. `-keys` honours `--config`, so `webtuidb --config new.toml -keys` checks a file before you use it.

## The keymap editor

{{< ui tui="configuration-keymap-rebind" web="configuration-keymap-rebind" >}}Giving `commit` the key `x` is refused in both UIs: `this takes the key from quick_filter (x): pick another key`. Nothing is written.{{< /ui >}}

### Terminal

{{< key "C-d ," >}}, then {{< key "Tab" >}}. The **Key bindings** tab lists every binding grouped by context (alphabetical by context, then by action), each as key, action name, description. Actions you have rebound carry a `●` after the description.

| Key | What it does |
|---|---|
| {{< key "j" >}} {{< key "k" >}} / arrows | move |
| {{< key "/" >}} | filter by action, description or key |
| {{< key "Enter" >}} or {{< key "Space" >}} | rebind the action under the cursor |
| {{< key "Tab" >}} | back to the settings |
| {{< key "e" >}} | open `config.toml` in `$EDITOR` |
| {{< key "Esc" >}} | close |

The **Key for &lt;action&gt;** form shows the current key in the `alt+x` / `ctrl+g` spelling and reminds you that it is pressed after the prefix unless the binding is a no-prefix one. Type one key (any spelling above; `-n ` in front for no prefix). An empty field restores the default. The new keymap is built first as a dry run, so a conflict shows in the form with a `✗` and nothing is saved. On success the file is rewritten (after the comments warning, if the file has comments), the keymap is replaced at once and the dialog says `commit rebound. …`.

### Web

**Settings** → **Keymap**. One row per action: its description, its name, and a text field with its keys separated by spaces, written in the config spelling (`alt+q`, `ctrl+d`, `P`), with `-n` before a key that needs no prefix. The search box filters by action name or description.

Edit the field and press {{< key "Enter" >}} or the row's **Save**. A conflict is shown under the row and nothing is written; an empty field is refused with *Give the action at least one key.* After a save the page reloads the keymap at once (no restart needed) and says *Key saved*.

{{< callout type="warning" >}}
In this version, saving a key from the web keymap editor writes only that one action to `[keys]`: other actions you had rebound in `config.toml` lose their custom keys and go back to the defaults. Until this is fixed, rebind several actions in the terminal editor or in the file. Both editors also drop comments from `config.toml`.
{{< /callout >}}

### What both editors refuse

The editors are stricter than the file. Besides every error a hand-written `[keys]` gets, they refuse a key that is already another action's, with `this takes the key from <action> (<key>): pick another key`, because saving it would silently remove that action's binding. They also refuse a key that an installed plugin's shortcut uses.

## Plugin shortcuts

A plugin file (`~/.config/webtuidb/plugins/*.toml` or `*.js`) can declare a `key`. It is bound in the `global` context as the action `plugin:<name>`, after the prefix, and you can rebind it under `[keys]` like any action (quote the name: `"plugin:hello" = "M-h"`).

A plugin shortcut that clashes with a default, a `[keys]` entry or another plugin, or that does not parse, is skipped; the other plugins and the rest of the keymap still load. The terminal shows `plugin shortcut skipped: <action> (<key>): <reason>` in its status line, `webtuidb -keys` prints it to stderr, and the web shows *Plugin shortcut conflicts: …* once when a workspace opens. A `[keys]` override for a plugin that is not installed is ignored, so removing a plugin never breaks startup.

## In the browser

The web runs the same prefix state machine over the keymap from `GET /api/meta`, so your prefix and `[keys]` apply there too.

- **⌘ (Cmd) is never a shortcut.** A key pressed with Cmd goes to the browser, so its own shortcuts keep working.
- **Alt / Option** is matched on the physical key, so {{< key "M-f" >}} works on a Mac even though Option+f types `ƒ`, and {{< key "M-K" >}} (Option+Shift+k) is read as `M-K`, as in the terminal.
- **Keys the browser or the OS keeps never reach the page.** Chrome and Firefox handle Ctrl+W, Ctrl+T, Ctrl+N and Ctrl+Tab themselves, and macOS uses Ctrl+Space to switch input sources. The defaults avoid them: tab actions sit behind the prefix ({{< key "C-d w" >}}, {{< key "C-d ]" >}}), and no default uses Ctrl+W. Do not bind them yourself. `complete` ({{< key "C-d C-Space" >}}) may not reach the page on macOS; the editor also opens completions as you type (`[editor] autocomplete`). The full table is in [Shortcuts → Keys the browser keeps](../../reference/shortcuts/#keys-the-browser-keeps).
- While the passcode lock screen is up, no shortcut runs.

## Settings

`[general] prefix` and `[keys]`: [Settings reference](../settings-reference/#general). Every action and default key: [Shortcuts](../../reference/shortcuts/).
