---
title: Settings reference
description: Every key of config.toml with its type, default, allowed values, meaning, and which UI honours it.
weight: 20
---

This page lists every key `config.toml` accepts, one table per TOML table, in the order the settings editor shows them. Defaults are the built-in values webtuidb uses when a key is absent. A key that is not listed here is an error at start (`unknown setting "…"`); see [The config file](../config-file/#what-makes-the-file-invalid).

**UI** says which front end reads the key: *both*, *terminal* or *web*. When only one UI reads it, the reason is given. **Editor** says whether the settings editor ({{< key "C-d ," >}}) offers it: *yes*, *file* (only in the file), or *read-only in web* (the terminal form edits it, the web form shows it but does not write paths on the server).

Types: `bool` is `true`/`false`; `int` a whole number; `string` text; `list` a TOML array of strings (typed comma-separated in the settings editor); `enum` one of the listed values.

{{< callout type="info" >}}
After a save in the terminal settings editor the theme, the key bindings and most options apply at once; editor options apply to tabs opened afterwards; `[mcp]` and `[web]` need a restart. In the browser only the theme applies at once; restart the server for the rest.
{{< /callout >}}

## `[general]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `prefix` | string | `"C-d"` | any key name (not `Esc`) | The prefix key, tmux style. Every prefixed action is this key, then the action's key. | both (the web reads the resolved prefix from `/api/meta`) |
| `reopen_workspaces` | bool | `true` | | Reopen the tabs of the last session when you reconnect. | both |
| `new_tab_opens` | enum | `"query"` | `query`, `empty` | What a new tab opens: a query editor or an empty tab. | both |
| `query_timeout_secs` | int | `300` | `>= 0`; `0` = no limit | Cancel queries that run longer than this many seconds. | both |
| `keep_alive` | bool | `true` | | Ping idle connections so servers do not drop them. | both |
| `row_limit` | int | `300` | `> 0` | Rows fetched per page in the grid. | both |
| `show_database_name` | bool | `false` | | Show the database name on the connection stack. | both |
| `connection_sort` | enum | `"name"` | `name`, `tag`, `driver`, `recent` | Order of the connection list: by name, tag, driver, or most recently connected first. | both |
| `recent_files` | int | `10` | | How many opened SQL files to remember for **Open a recent file** ({{< key "C-d M-O" >}}). | both |
| `show_connection_name` | bool | `false` | | Show the connection name on the connection stack. | both |
| `estimate_count_over` | int | `1000000` | | Above this many rows the grid shows the planner's estimate instead of counting; {{< key "C-d #" >}} counts exactly. | both |
| `default_safe_mode` | string | `"silent"` | `silent`, `alert1`, `alert2`, `safe1`, `safe2` | Safe mode for new connections (see [Safety](../../safety/)). Edited as text in the settings editor. | both |
| `log_original_values` | bool | `false` | | Keep the previous values of edited rows in the query history (stored as CSV next to the statement). | both |
| `hints` | list | `[]` (built-in set) | action names, `plugin:<name>`; no repeats, no empty entries | Actions shown in the terminal status line, in order. Empty = the built-in hints for the focused pane. | terminal (the web has a toolbar, `[web] toolbar`, instead) |

## `[editor]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `autocomplete` | bool | `true` | | Offer completions while typing. | both |
| `uppercase_keywords` | bool | `true` | | Uppercase SQL keywords you type. | both |
| `close_braces` | bool | `true` | | Insert the closing bracket or quote automatically. | both |
| `complete_key` | enum | `"enter_or_tab"` | `enter_or_tab`, `enter`, `tab` | Key that accepts a completion. | both |
| `indent` | enum | `"spaces"` | `spaces`, `tabs` | Indent with spaces or tabs. | both |
| `indent_width` | int | `4` | | Spaces per indent level. | both |
| `result_mode` | enum | `"tabs"` | `tabs`, `append` | Run results open in tabs, or are appended below the previous ones. | both |
| `return_on_error` | bool | `false` | | Return the cursor to the editor when a query fails. | both |
| `query_params` | bool | `false` | | Ask for values of `:named` parameters before running. | both |
| `param_format` | enum | `"colon"` | `colon` (`:name`), `dollar` (`$name`), `at` (`@name`), `brace` (`{name}`), `mustache` (`{{name}}`), `question` (`?`) | Parameter syntax recognised in queries. | both |
| `auto_save_queries` | bool | `true` | | Save the editor text to disk as you type. | both |
| `auto_save_tabs` | enum | `"first"` | `none`, `first`, `all` | Which editor tabs are restored on reconnect (stored in `state.json`). | both |
| `wrap` | bool | `true` | | Wrap long lines. | both |
| `indent_type` | enum | `"standard"` | `standard`, `tabular_left`, `tabular_right` | Indent style of the SQL formatter (beautify, {{< key "C-d i" >}} in the editor). | both |
| `add_space` | bool | `true` | | Add a space after accepting a completion. | both |
| `prefix_schema` | bool | `false` | | Complete tables as `schema.table`. | both |
| `params_in_strings` | bool | `false` | | Look for parameters inside string literals. | neither: accepted in `config.toml` but not implemented yet (reserved) |
| `complete_tables` | bool | `true` | | Complete table and column names. | both |
| `complete_functions` | bool | `true` | | Complete function names. | both |
| `complete_keywords` | bool | `true` | | Complete SQL keywords. | both |
| `run_default` | enum | `"current"` | `current`, `all` | What **Run** does: the statement under the cursor, or all statements. | both |
| `highlight_current` | bool | `true` | | Highlight the line the cursor is on. | both |

## `[table]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `alternating_rows` | bool | `true` | | Shade every other row of the grid. | both |
| `filter_default_column` | enum | `"pk"` | `pk`, `any`, `raw` | Column a new filter starts on: the primary key, *any column*, or a raw SQL condition. | both |
| `filter_default_operator` | enum | `"="` | `=`, `contains` | Operator a new filter starts with. | both |
| `filter_state` | enum | `"remember"` | `remember`, `show`, `hide` | Filter bar visibility when a table opens: as you left it for that table, always shown, or always hidden. | both |
| `default_sort` | enum | `"none"` | `none`, `pk_asc`, `pk_desc` | Sort a table opens with. A sort you remove on a table stays removed for it. | both |
| `filter_column_sort` | enum | `"ordinal"` | `name`, `ordinal` | Order of the columns in the filter's column list. | both |
| `auto_pretty_json` | bool | `false` | | Pretty-print JSON cells in the grid and row detail. | both |
| `decode_spatial` | bool | `true` | | Show geometry cells as WKT instead of hex. | both |
| `image_preview` | bool | `true` | | Recognise image bytes and preview them in quick look. | both |

## `[csv]`

Used by CSV export, CSV import and **Copy as CSV**.

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `delimiter` | string | `","` | exactly one character, not a line break | Field separator. | both |
| `quote` | string | `"\""` | one printable ASCII character, not the delimiter or a line break | Quote character. | both |
| `line_break` | string | `"\n"` | | Line break written on export (`"\r\n"` for Windows). | both |
| `decimal` | string | `"."` | | Decimal separator. | both |
| `null_as` | string | `""` | | Text written for NULL on export, and read back as NULL on import. | both |
| `quote_mode` | enum | `"needed"` | `always`, `needed` | Quote every field, or only fields that need it. | both |

## `[sidebar]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `show_recent` | bool | `true` | | Show the **Recent** section. | both |
| `show_functions` | bool | `true` | | Show functions. | both |
| `show_sequences` | bool | `true` | | Show sequences. | both |
| `show_types` | bool | `true` | | Show types. | both |
| `matching` | enum | `"fuzzy"` | `fuzzy`, `contains` | How the sidebar search matches names. | both |
| `show_columns` | bool | `false` | | Show columns under each table. | both |
| `sort_columns` | bool | `false` | | Sort columns alphabetically instead of by position. | both |
| `load_system_schemas` | bool | `false` | | Load `pg_catalog`, `information_schema` and similar. | both |
| `lazy_load` | bool | `true` | | Load a schema's objects only when it is expanded. | terminal (the web loads the objects of one schema at a time by design) |

## `[theme]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `name` | enum | `"dark"` | `dark`, `light`, `auto`, `silver-dark`, `jade-dark`, `polaris-dark`, `sunset-dark`, `sunset-light`, or an installed theme | Colour theme. `auto` follows the terminal background / the browser's colour scheme. Spaces and `_` are read as `-` (`"Silver Dark"`). | both |
| `file` | string | `""` | a path; `~/` and `$VARS` are expanded | A theme TOML whose `[theme.colors]` go on top of `name`. Editor: read-only in web. | both |
| `colors` | table | `{}` | palette keys, `tag.<name>`, `driver.<name>`; values `#rgb`, `#rrggbb` or `0`–`255` | Colour overrides on top of the theme and the file. Editor: file. | both (the web uses a subset) |

All palette keys and how the web maps them: [Themes](../themes/).

## `[ai]`

The AI assistant ({{< key "C-d A" >}}). Not in the settings editor; edit the file.

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `provider` | enum | `"anthropic"` | `anthropic`, `openai` | Wire format of the API. `openai` also covers any OpenAI-compatible endpoint. | both |
| `model` | string | `"claude-opus-5-5"` | | Model name sent to the API. | both |
| `api_key` | string | `""` | | The API key in plain text. Makes webtuidb chmod the file to `0600`. | both |
| `api_key_cmd` | string | `""` | | A command whose standard output (trailing newline removed) is the key. Run directly, without a shell; quotes group arguments. Time limit 30 s. When set, it wins over `api_key`. | both |
| `base_url` | string | `""` | | An OpenAI-compatible endpoint, for example `http://localhost:11434/v1` for Ollama. | both |

### `[ai.providers.<name>]`

Named alternatives you can switch to per chat. The top-level `[ai]` keys are the default; each named provider has the same five keys (`provider`, `model`, `api_key`, `api_key_cmd`, `base_url`) and inherits every key it leaves empty from `[ai]`. Two details:

- A provider's own `api_key` is not shadowed by the default's `api_key_cmd`, and its own `api_key_cmd` drops the default's `api_key`.
- A provider with `base_url` but no `provider` is treated as `openai` when the URL path contains `/v1` or the host is not `anthropic.com`; otherwise it is refused: `ai.providers.<name>: base_url is set without provider; add provider = "anthropic" or "openai"`.

```toml
[ai]
provider = "anthropic"
api_key_cmd = "op read op://Private/anthropic/key"

[ai.providers.local]
base_url = "http://localhost:11434/v1"
model = "qwen2.5-coder"
```

## `[mcp]`

The MCP server that lets an AI client query your saved connections. Not in the settings editor.

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `enabled` | bool | `false` | | Serve MCP over HTTP while the terminal UI or the web server runs. (`webtuidb --mcp` serves over stdio regardless.) | both |
| `port` | int | `51003` | | Port of the HTTP listener on `127.0.0.1`. | both |
| `listen` | string | `""` | `host:port` | Listen address; empty = `127.0.0.1:<port>`. Set it to serve to other machines. | both |
| `tls_cert` | string | `""` | path | HTTPS certificate for the MCP listener; set together with `tls_key`. | both |
| `tls_key` | string | `""` | path | HTTPS private key. | both |
| `approval_for_unsafe` | bool | `true` | | A statement that is not read-only waits for your approval in the UI; without a UI to ask, it is refused. | both |

### `[[mcp.token]]`

One table per client token. Over HTTP the client sends `Authorization: Bearer <value>`; `--mcp` over stdio uses the first token's scope, or no limit when there is none.

| Key | Type | Default | Allowed | Meaning |
|---|---|---|---|---|
| `name` | string | | unique | Shown in the MCP activity log and approval prompts. |
| `value` | string | | | The secret the client sends. Makes webtuidb chmod the file to `0600`. |
| `scope` | enum | | `read-only`, `read-write`, `full` | `read-only` runs only read-only statements; `read-write` also writes but refuses DDL and statements it cannot classify; `full` allows everything. |
| `connections` | list | `[]` | connection names | Saved connections the token may use; empty = all. |

## `[queries]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `shared_dirs` | list | `[]` | folder paths; `~` is expanded, relative paths are made absolute | Extra folders of saved queries, listed beside your own and read-only (for example a team folder in git). Editor: read-only in web. | both |

## `[export]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `filename` | string | `"{table}-{date}"` | placeholders `{connection}` `{database}` `{table}` `{tag}` `{date}` `{time}` | Export file name pattern. Empty = the default. | both |
| `header` | bool | `true` | | Write a header row in CSV exports. | both |

## `[web]`

Read by the web server (`webtuidb --frontend`).

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `tls_cert` | string | `""` | path | Serve HTTPS when both cert and key are set. Editor: read-only in web. | web (the terminal UI serves no HTTP) |
| `tls_key` | string | `""` | path | TLS private key. Editor: read-only in web. | web |
| `password` | string | `""` | | A login page in front of the token; empty = token only. `WEBTUIDB_PASSWORD` overrides it. Makes webtuidb chmod the file to `0600`. Editor: file. | web (the terminal UI has no login) |
| `font_size` | int | `0` | px; `0` = default | Font size of the page. | web (the terminal uses its emulator's font) |
| `line_height` | string | `""` | a CSS value; empty = default | Line height of the page. | web (the terminal uses its emulator's line height) |
| `notifications` | bool | `false` | | Desktop notifications when a backup or import finishes (the browser asks for permission). | web (the terminal rings the bell / sends OSC 9 instead) |
| `auto_hide_scrollbars` | bool | `true` | | Show scrollbars only while scrolling. | web (the terminal draws its own scroll rails) |
| `toolbar` | list | `[]` (built-in set) | action names, `plugin:<name>`, `history`, `saved_queries`; no repeats | Buttons of the workspace toolbar, in order. Empty = `new_query_tab`, `history`, `saved_queries`, `open_anything`, `help`, `disconnect`. | web (the terminal has `[general] hints` instead) |

## `[security]`

| Key | Type | Default | Allowed | Meaning | UI |
|---|---|---|---|---|---|
| `passcode` | string | `""` | a bcrypt hash (`$2…`) | The lock screen passcode. Make the line with `webtuidb -set-passcode` (it reads the passcode twice and prints `[security]` / `passcode = "…"`). Makes webtuidb chmod the file to `0600`. Editor: file. | both |
| `lock_after_mins` | int | `0` | `>= 0`; `0` = never | Lock after this many idle minutes. {{< key "C-d M-L" >}} (`lock`) locks at once. | both |

## `[[tag]]`

Custom connection tags with colours. The built-in tags are `local`, `testing`, `development`, `staging` and `production`; a `[[tag]]` with one of those names recolours it. Not in the settings editor.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `name` | string | required, unique | Tag name, as used in a connection's `tag`. |
| `color` | string | `""` | Colour of the tag; empty = the palette's own colour for that name (`tag.<name>` in `[theme.colors]`). |

```toml
[[tag]]
name = "qa"
color = "#bb9af7"
```

## `[[source]]`

Extra folders of connection files. Not in the settings editor.

| Key | Type | Meaning |
|---|---|---|
| `path` | string | A folder; every `*.toml` in it is read in `connections.toml` format, in name order. `$VARS` and a leading `~/` are expanded. Connections from it are read-only in both UIs, and a name already in `connections.toml` wins. See [connections.toml](../connections-file/#extra-connection-folders-source). |

## `[keys]`

Action name → key or list of keys. Edited in the keymap editor of both UIs. Syntax: [The config file → `[keys]` syntax](../config-file/#keys-syntax); key names and contexts: [Key bindings](../key-bindings/); every action and its default key: [Shortcuts](../../reference/shortcuts/).
