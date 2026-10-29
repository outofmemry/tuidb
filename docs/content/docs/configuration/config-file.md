---
title: The config file
description: Where config.toml lives, how it is layered over the defaults, the settings editor in the terminal and the browser, and the [keys] syntax.
weight: 10
---

`config.toml` holds every preference webtuidb has: the prefix key, editor and grid behaviour, CSV format, theme, AI and MCP setup, web server options and your key bindings. It is optional. Without it webtuidb runs on its built-in defaults, and you only write the keys you want to change.

{{< ui tui="configuration-settings" web="configuration-settings" >}}The settings editor ({{< key "C-d ," >}}) on its first section, `[general]`.{{< /ui >}}

## Where it lives

| What | Path |
|---|---|
| Default location | `~/.config/webtuidb/config.toml` |
| With `XDG_CONFIG_HOME` set | `$XDG_CONFIG_HOME/webtuidb/config.toml` |
| Any other file | `webtuidb --config /path/to/config.toml` |

- `XDG_CONFIG_HOME` (and `XDG_DATA_HOME` for the data folder) is used only when it is an absolute path; a relative value is ignored, as the XDG spec says.
- When neither `HOME` nor absolute XDG variables are set, webtuidb refuses to start: `cannot find your home directory (HOME is unset); set HOME or XDG_CONFIG_HOME/XDG_DATA_HOME to absolute paths`.
- `--config` changes only the config file. `connections.toml`, the themes and plugins folders and the data folder stay where they are. The settings editor in both UIs writes back to the file the program was started with, and its title line (terminal) or the **Copy config path** button (web) shows which file that is.
- `webtuidb -mcp-config <client>` adds `--config <absolute path>` to the printed MCP entry when you passed `--config`, so the MCP server reads the same file.

### Migration from tuidb

webtuidb was called tuidb before. On start, if `~/.config/tuidb` (or `$XDG_CONFIG_HOME/tuidb`) exists and `~/.config/webtuidb` does not, the old folder is renamed to the new name; the same happens for `~/.local/share/tuidb`. Your connections, settings, history, saved queries and UI state come along. If the new folder already exists, it wins and the old one is left untouched. A failed move stops the program with `move <old> to <new>: <error>` instead of starting empty, so it never looks as if your data vanished.

## How the defaults are layered

1. webtuidb starts from its built-in defaults (listed in the [settings reference](../settings-reference/)).
2. It reads `config.toml` on top. Every key you set replaces the default; every key you leave out keeps it. A missing file is not an error.
3. A few values are normalised after reading:
   - `[theme] file` has `$VARIABLES` and a leading `~/` expanded.
   - When `[theme] name` is not a built-in palette, it is looked up in the themes folder and that theme's file is used (see [Themes](../themes/)).
   - An empty `[export] filename` means the default pattern `{table}-{date}`.
4. If the file holds a plaintext secret (`[web] password`, `[ai] api_key`, an `api_key` under `[ai.providers.<name>]`, an `[[mcp.token]]` value, or `[security] passcode`) and other users can read it, webtuidb changes it to mode `0600`. If it cannot, it stops with `<path> holds secrets and is readable by other users; chmod 600 it`.
5. The whole result is validated (see below). Any error stops the program.

The environment can override two web settings at start: `WEBTUIDB_PASSWORD` replaces `[web] password`, and `WEBTUIDB_TOKEN` is used when `--token` is not given (see [Web UI](../../web-ui/)).

### What makes the file invalid

webtuidb never ignores a setting it does not understand: a typo would silently change nothing. These all stop the program (terminal UI, web server, `-keys`, `-theme`, `--mcp`) with one line on stderr:

| Problem | Message |
|---|---|
| TOML syntax error | `webtuidb: <path>: toml: line 3: expected '.' or '=', but got '/' instead` |
| Unknown key or table | `webtuidb: <path>: unknown setting "general.prefx"` |
| Value not in the allowed list | `webtuidb: table.filter_state: "open" is not one of remember\|show\|hide` |
| `row_limit` of 0 or less | `general.row_limit must be > 0` |
| Negative timeout or lock time | `general.query_timeout_secs must be >= 0 (0 = no limit)`, `security.lock_after_mins must be >= 0 (0 = never)` |
| Bad safe mode | `general.default_safe_mode: …` (the safe-mode parser's message) |
| CSV delimiter or quote not one character | `csv.delimiter: ";;" must be exactly one character` |
| Unknown action in `hints` or `toolbar` | `general.hints: "nope" is not an action name (see the help screen for the names)` |
| Same action listed twice, or an empty entry | `web.toolbar: "help" is listed twice`, `general.hints: an action name must not be empty` |
| Passcode that is not a bcrypt hash | `security.passcode must be a bcrypt hash; make one with webtuidb -set-passcode` |
| Half of a TLS pair | `web.tls_cert and web.tls_key must be set together` (same for `mcp.`) |
| Bad AI provider name | `ai.providers.local: provider "ollama" is not one of anthropic\|openai` |
| Bad MCP token | `mcp.token "ci": duplicate name`, `mcp.token "ci": scope "admin" is not one of read-only\|read-write\|full` |
| Bad `[[tag]]` | `[[tag]] needs a name`, `duplicate [[tag]] name "qa"` |
| Bad `[keys]` entry | see [`[keys]` syntax](#keys-syntax) below |

Unknown keys are allowed in two places only: under `[keys]` (every name is checked against the action list instead) and under `[theme.colors]` (checked at start, reported as a warning; see [Themes](../themes/#colour-overrides)).

`webtuidb -version`, `-man`, `-changelog`, `-bugreport`, `-check-update`, `-set-passcode`, `-theme-install`, `-theme-update`, `-completion` and `-mcp-config` do not load the config, so they still work while it is broken. `-bugreport` prints your config with every secret masked, for an issue report.

## The settings editor

The `settings` action opens a form over `config.toml` in both UIs. It covers the sections `[general]`, `[editor]`, `[table]`, `[csv]`, `[sidebar]`, `[theme]`, `[export]`, `[web]`, `[queries]` and `[security]`, plus a keymap editor for `[keys]`. The list of fields comes from one place in the engine (`config.Schema`), so both UIs offer the same keys, the same types, the same allowed values and the same one-line help.

Not in the form: `[ai]`, `[ai.providers.<name>]`, `[mcp]`, `[[tag]]`, `[[source]]`, `[theme.colors]`, `[web] password` and `[security] passcode`. Edit those in the file (`webtuidb -set-passcode` prints the passcode line).

### How to open it

| | Terminal | Web |
|---|---|---|
| Key | {{< key "C-d ," >}} (action `settings`), on the connection list and in a workspace | the same {{< key "C-d ," >}} |
| Elsewhere | in a workspace, Open Anything ({{< key "C-d p" >}}) → **Settings** | the **Settings** button on the Connections page |

### In the terminal

{{< ui tui="configuration-settings-search" >}}{{< key "/" >}} filters by key name and help text: here `csv` shows the `[csv]` table and `export.header`, whose help mentions CSV.{{< /ui >}}

The dialog lists every setting as `section.key  value`, grouped under `[section]` headers. The help line of the setting under the cursor is shown under the list. A changed but unsaved value carries a `*`, and the title says `Settings · N unsaved`.

| Key | What it does |
|---|---|
| {{< key "j" >}} {{< key "k" >}}, {{< key "Down" >}} {{< key "Up" >}}, {{< key "C-n" >}} {{< key "C-p" >}} | move; headers are skipped |
| {{< key "PgDown" >}} {{< key "PgUp" >}} | move ten rows |
| {{< key "Enter" >}} or {{< key "Space" >}} | edit the value: a `true`/`false` setting flips; a setting with a fixed list of values opens a picker (the current value is marked `current`, type to filter); text, number and list settings open a small form |
| {{< key "s" >}} or {{< key "C-s" >}} | save every pending change |
| {{< key "r" >}} | revert the pending change of the setting under the cursor |
| {{< key "/" >}} | filter by key name or help text; {{< key "Enter" >}} keeps the filter, {{< key "Esc" >}} clears it |
| {{< key "Tab" >}} | switch to the **Key bindings** tab and back |
| {{< key "e" >}} | open `config.toml` in `$VISUAL`, else `$EDITOR`, else `vi` (the program is suspended while the editor runs) |
| {{< key "Esc" >}} or {{< key "q" >}} | close; with unsaved changes it first asks **Discard N unsaved changes?** |

{{< ui tui="configuration-settings-enum" web="configuration-settings-theme" >}}Settings with a fixed set of values open a picker in the terminal and a drop-down in the browser. Here `table.filter_state` (terminal) and `theme.name` (web).{{< /ui >}}

In a number field the form refuses text that is not a whole number (`general.row_limit: "ten" is not a whole number`). List settings (`general.hints`, `web.toolbar`, `queries.shared_dirs`) are typed comma-separated; spaces around commas and empty entries are dropped.

{{< ui tui="configuration-settings-pending" web="configuration-settings-pending" >}}One unsaved change: `table.alternating_rows` switched off. The terminal marks it with `*` and counts it in the title; the web enables **Save 1**.{{< /ui >}}

When you save, the whole result is validated first. If it would not load, nothing is written and the message is shown in the dialog (for example `general.hints: "nope" is not an action name (see the help screen for the names)`). After a successful save the terminal reloads the config at once and says: `saved N settings. Theme, keys and most options are live; editor options apply to new tabs; restart to apply the MCP and web settings.`

### In the browser

The web dialog shows the sections as a list on the left and the fields of one section on the right, each with its help text underneath: checkboxes for `true`/`false`, a drop-down for fixed values, a number field for numbers and a text field for the rest (lists show `comma separated` as placeholder). The last section, **Keymap**, is the keymap editor.

- **Save N** writes the changed fields of all sections in one go. A field you changed back to its old value does not count.
- If the server refuses a value, the message is shown above the fields, the dialog jumps to the section of the field it names, and that field is outlined with the message under it.
- After saving, a theme change applies to the page at once. Any other change shows **Saved. Restart webtuidb for these to apply.** Only the settings dialog and `GET /api/meta` see the new values right away; the rest of the server keeps the values it started with until you restart it.
- **Copy config path** copies the path of the file being edited.
- Settings that name a file or folder on the server are shown read-only, with the tooltip *Set in the config file; the web UI does not write paths*: `queries.shared_dirs`, `theme.file`, `web.tls_cert`, `web.tls_key`. The server also refuses them in a direct API call: `queries.shared_dirs names a path on the server and can only be edited in config.toml`.
- There is no search box for settings in the web dialog (the keymap editor has one).

### Saving rewrites the file

{{< callout type="warning" >}}
Both editors save by reading `config.toml`, changing the values and writing the whole file again. **Comments in the file are not kept**, and keys come out in the encoder's order. The new file starts with the line `# Rewritten by webtuidb settings; comments from earlier versions of this file are not kept.` Tables the form does not cover (`[ai.providers.*]`, `[[tag]]`, `[[mcp.token]]`, …) are kept with their values.

The terminal warns first when the file has comments of its own: **Saving drops the comments in config.toml**, with **Save anyway** or {{< key "n" >}}, then {{< key "e" >}} to edit the file by hand in `$EDITOR`, which keeps them. The web saves and then shows *comments in config.toml were not kept*.
{{< /callout >}}

The new file is written to a temporary file in the same folder with mode `0600`, checked by loading it, and then renamed over `config.toml`, so a crash or a refused value never leaves a half-written file.

## `[keys]` syntax

`[keys]` maps an action name to the key (or keys) that run it. The prefix itself is `[general] prefix`.

```toml
[general]
prefix = "C-a"                    # tmux-style key name

[keys]
commit  = "x"                     # one key: replaces every default key of commit
run_all = ["M-Enter", "-n F5"]    # a list binds several keys
"plugin:hello" = "M-h"            # a plugin's shortcut (quote the name: it has a colon)
```

- **Key names** are tmux-style: `C-x` (Control), `M-x` (Alt / Option), `S-Tab`, `F5`, `Enter`, `Space`, `]`, `P`. The full grammar is on [Key bindings](../key-bindings/#key-names).
- **A list** gives the action several keys. A value that is neither a string nor a list of strings is an error: `<path>: [keys] commit must be a string or a list of strings`.
- **`-n `** before a key means *no prefix*: the key works on its own, like tmux `bind -n`.
- **Rebinding replaces the default.** Once you set an action, none of its default keys stay bound, in any context it has.
- **Taking a key from another action** is allowed in the file: your binding wins and the other action loses that key. It is reported at start, not refused (see below). The settings editors refuse such an edit instead.
- **Plugin shortcuts** are actions named `plugin:<name>`. Overriding the key of a plugin that is not installed is ignored, so removing a plugin never breaks startup.

These are errors and stop the program, naming the action:

| Problem | Message |
|---|---|
| Unknown action | `[keys] unknown action "fly"` |
| Unparsable key | `[keys] commit: bad key "C-Z-x": unknown modifier "Z"` |
| Empty list | `[keys] commit: an empty list would remove the action; give it a key` |
| Binding `Esc` | `[keys] quit: esc cancels the prefix and cannot be bound` |
| Binding the prefix key | `[keys] commit: C-d is the prefix key and cannot be bound` |
| `Esc` as the prefix | `prefix: esc cancels the prefix and cannot be the prefix` |
| Two of your bindings on one key in one context | `[keys] next_tab and prev_tab are both bound to M-q in global` |

### The startup report

When a `[keys]` entry takes a default key from another action, webtuidb still starts, and says which action lost its key. The line is printed to stderr (also by `webtuidb -keys`) and shown as a notice in the terminal UI's status line:

{{< ui tui="configuration-startup-error" >}}`webtuidb --config bad.toml -keys` with `commit = "x"`: the grid's quick filter loses its key, and the listing shows `commit` on `x`.{{< /ui >}}

```text
webtuidb: [keys] commit = "x" takes the key from quick_filter (grid); quick_filter is now unbound
```

If the action still has keys in other contexts, the line ends with `… is now unbound in grid` instead. Plugin shortcuts that clash with a binding are skipped and listed as `webtuidb: plugin shortcut skipped: …` (see [Key bindings](../key-bindings/#plugin-shortcuts)).

The same rules apply to the keymap editor in both UIs; see [Key bindings → the keymap editor](../key-bindings/#the-keymap-editor).

## Settings

Every key, its default and which UI reads it: [Settings reference](../settings-reference/).
