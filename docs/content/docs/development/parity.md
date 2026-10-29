---
title: Terminal and web parity
description: How webtuidb keeps the terminal UI and the web UI identical, what make parity asserts, and how to add an action, a setting, a menu entry or a dialog so both front ends get it.
weight: 30
---

webtuidb has one engine and two front ends, and the project rule is that both behave the same. The rule is enforced by tests: `make parity` fails when a key binding, a menu entry, a setting, a dialog or a capability check exists in one front end and not in the other. This page explains what the tests compare and walks through the four most common changes.

{{< ui tui="development-parity-help" web="development-parity-help" >}}
The same key binding list in both front ends, opened with {{< key "C-d ?" >}}. Rows, action names and descriptions come from one table (`internal/keymap/action.go`). The terminal lists the focused pane's section first (the sidebar here); the web dialog lists Global first.
{{< /ui >}}

## The rule

From `CLAUDE.md`:

- Any change that mentions a shortcut, key binding, keymap, hotkey, prefix key or "when I press X" is made in **both** UIs in the same pass, even if only one is named.
- The terminal UI and the web UI show the **same** help listing: same action names, same descriptions, same order.
- A shortcut that cannot work in one UI (a terminal key the browser swallows, for example) is not skipped silently: say so, and pick the closest key for that UI. The browser cases are listed on the [Shortcuts]({{< relref "/docs/reference/shortcuts" >}}) page.

## `make parity`

```sh
make parity
# go test ./internal/menus/... ./internal/config/... -run 'Parity|TestHolds' && go test ./internal/web ./internal/ui -run Parity
```

CI runs it as its own step (see [Testing]({{< relref "testing" >}})). It is quick (a few seconds). The tests it runs, by what they guarantee:

### 1. Actions: keymap, `SUPPORTED`, handlers

`internal/web` `TestParityActions`:

- The set `SUPPORTED` in `web/src/routes/Workspace.tsx` equals the set of actions in the keymap defaults (`keymap.DefaultBindings()`), plus any `plugin:<name>` entries. An action missing from `SUPPORTED` would be greyed out in the web help; an entry that is not a keymap action is an error. The test reads `SUPPORTED` with a regular expression (`internal/web/parity.go`), so it has to stay a literal `new Set([...])` of quoted names, with the generated `tab_1` to `tab_9` written as `...Array.from({ length: 9 }, (_, i) => `tab_${i + 1}`)`.
- Every keymap action has a handler in the terminal code: the quoted name appears in `internal/ui` (non-test files).
- Every keymap action has a handler in the web code: the quoted name, or an object key in a handler table (`quick_look: () => …`, as in `keyActions` in `DataTab.tsx`), appears in `web/src`. The `SUPPORTED` list itself is cut out first, so it cannot satisfy this on its own.
- The `tab_N` family is handled in both.

Two maps in the test, `onlyTUI` and `onlyWeb`, can exempt an action that one UI genuinely cannot answer, with the reason. Both are empty: every action is implemented in both UIs.

`TestSupportedActionsParsesWorkspace` checks that the parser still reads `Workspace.tsx` (it must find `help`, `commit`, `tab_1`, `tab_9`, `close_pane`).

### 2. Help listings: `-keys`, `/api/meta` and both help screens

- `internal/web` `TestParityHelpListing`: the `keymap` that `GET /api/meta` returns (prefix and bindings, in table order) equals the engine's resolved keymap; every binding appears in the text of `webtuidb -keys` with the same action, key and description, in the same relative order, for every context; and the web `Help` component does not call `.sort(`, so it renders the meta order unchanged.
- `internal/ui` `TestParityHelpListing`: each row of the terminal help dialog (`?`) appears, in order and with the same action, key, `-n` marker and description, in the `[context]` section of `webtuidb -keys`, and the dialog does not re-sort when no search is active.

`-keys` prints, per context (`global`, `sidebar`, `grid`, `filterbar`, `structure`, `editor`, `results`, `console`), one line per binding: action padded to 26 columns, key padded to 10 (`-n ` in front of a key that needs no prefix), description.

### 3. Settings: `config.Parity` for every UI key

`internal/config/parity.go` lists every setting in the sections `general`, `editor`, `table`, `csv`, `sidebar`, `theme`, `export`, `web`, `queries` and `security`, with two booleans, `TUI` and `Web`, and a `Why` string:

```go
{"general.hints", true, false, "terminal status-line hint chips; the web has a toolbar ([web] toolbar) instead"},
{"web.font_size", false, true, "the terminal uses the terminal emulator's font"},
```

`TestParityCoversEveryKey` fails when:

- a field of those config structs has no entry (and no entry names a field that does not exist);
- an entry says the terminal honours the key but `internal/ui` (or `internal/export`, which the TUI uses for CSV and export settings) never references the Go field (`.FieldName`);
- an entry says the web honours it but neither `internal/web` (the field name) nor `web/src` (the field name or its camelCase, as a property or a quoted key) references it;
- a key one UI ignores has an empty `Why`.

`TestParityNineKeysBothUIs` pins a list of keys that were once terminal-only (`table.filter_state`, `general.new_tab_opens`, `editor.close_braces`, `sidebar.matching` and others) to "both UIs honour it". One setting, `editor.params_in_strings`, is listed as honoured by neither (accepted in `config.toml`, reserved).

### 4. Menus from one list

Every context menu is built from lists in `internal/menus/menus.go`; the terminal builds its pickers from them and `GET /api/meta` exports them as `menus` for the web.

- `internal/ui` `TestParityMenusMatchLists`: opens every menu the terminal builds and compares its labels with `menus.Labels(list, env)` for the same environment (object kind, capabilities, editable result, marked item). It covers the sidebar object menu for every kind with Triggers and Schemas on and off, the `+` menu with each of Views, MatViews, Functions and Schemas off, the database, console, welcome (connection list), tab, grid, column header, filter bar, structure (with and without `ReorderColumns`), result and process list menus.
- `internal/ui` `TestParityMenuActionsDispatched`: every item that is not marked `ui: "web"` names an action that `internal/ui` dispatches on, as a `case … "action"` clause or an `== "action"` comparison; a bare string literal does not count. `TestParityMatcherFailsWhenRemoved` proves the matcher itself fails when a dispatch is removed.
- `internal/web` `TestParityMenusFromMeta`: `/api/meta` carries every list, `web/src` reads each as `menus.<name>` (or through the `listOf(meta.menus, '<name>')` helper), and the connection page builds its menu from `menus.welcome`. The web keeps no copy of any list.
- `internal/menus` `TestHolds` pins the exact entries of the view menu, checks that `Triggers…` needs the capability, and that no list has an empty label or action or a duplicate label.

A menu item that only one UI can do carries `UI: "web"` or `UI: "tui"` and a `Why`; the other UI shows it greyed with the reason (`Add to metrics board…` is terminal-only, `Copy link` and `Open workspace in a new browser tab` are web-only, the jobs centre's `Open folder` and `Download` are one each).

The welcome screen is covered by the `welcome` menu above and by `TestParityBootstrapFailureReported` in `internal/web`: when a connection's bootstrap commands fail, the terminal shows a "Bootstrap command failed" dialog and the web returns the failures from `POST /api/sessions` as `bootstrapErrors`.

### 5. Dialogs: 31 pairs

`internal/menus/dialogs.go` lists `menus.Dialogs`, one `{Name, TUI, Web}` entry per dialog, 31 today: Set Value, quick filter modes, page settings, count exactly, clone, truncate options, delete with the typed name, the safe-mode gate and level picker, the "server is taking too long" prompt, bootstrap failure, import, export, backup, restore, users, process list, search in database, variables, find and replace, settings, keymap editor, jobs centre, passcode lock, assistant chat history, foreign key form, new database with engine options, new table, column header menu, Open Anything and the key bindings help.

`TestParityDialogs` (in `internal/menus`) reads every non-test `.go` file under `internal/ui` and every `.ts` and `.tsx` file under `web/src`, and requires that the `TUI` symbol (a function, type or method name, or a distinctive title string) occurs in the first and the `Web` symbol (a component name or title string) in the second. A name listed twice, or an entry with an empty side, is an error: if a UI lacks a dialog, the gap is named in the report instead of leaving the field blank.

### 6. Capability gating

An engine's `db.Capabilities` switch features on and off (`Indexes`, `Triggers`, `Functions`, `ProcessList`, `Users`, `Schemas`, `Document`, `Views`, `MatViews`, `Sequences`, `ReorderColumns`). `TestParityCapabilityGating` exists in both packages and checks each capability on and off:

- **Terminal** (`internal/ui`): with the capability off, the feature is hidden or refused with a toast. The Index view says it has no Index tab, the sidebar menu loses `Triggers…`, the `+` menu loses `New view…`, `New materialized view…`, `New function / procedure…` and `New schema…`, the Sequences section disappears, `process_list` and `users` answer "not supported", `show_json` answers that it is for document engines, and the structure menu loses `Move up` and `Move down`.
- **Web** (`internal/web`): the session info (`GET /api/sessions/{id}`) reports each capability as `caps.<Name>`; the gated routes answer `supported: false` (`/processes`, `/users`) or an error (`/users/plan`, `move_column` in `/plan/table`); and `web/src` reads every capability, either as `caps.<Name>` in a component or as a `cap:<Name>` predicate in a shared menu list.

## When `make parity` fails

The messages name both halves. For example:

```
keymap action "my_action" is missing from SUPPORTED in web/src/routes/Workspace.tsx (the help screen would grey it out)
keymap action "my_action" has no handler in internal/ui (no quoted "my_action")
dialog "My dialog": web/src has no "MyDialog": the web lacks it (the terminal has "func (a *App) myDialog(")
menus.grid "My entry": internal/ui never dispatches on "my_entry"
web/src never reads caps.Triggers and no shared menu has cap:Triggers: the web does not gate on it
```

Fix the side that is missing; do not weaken the test.

## Add an action (a key binding)

Terminal first, then web, in the same change (steps from `CLAUDE.md`):

1. **Keymap table.** Add `{"my_action", "x", Global, "what it does"}` to the `defaults` table in `internal/keymap/action.go` (or to `noPrefixDefaults` for a key that works without the prefix, like tmux `bind -n`). The fields are action name, canonical key (`ctrl+x`, `alt+x`, `X` for shift), context (`Global`, `Sidebar`, `Grid`, `Editor`, `Results`, `Structure`, `FilterBar`, `Console`) and description. The **description is what the help screens show**, in this table order. Pick a key that is free in that context and in `Global`: `TestDefaultsAreConflictFree` builds the whole table and fails on a duplicate. Keys the terminal cannot receive (`C-[` is Esc, `C-i` is Tab, `C-d` is the prefix) and keys a browser keeps are the reason some bindings use `(`, `)` or `M-` forms.
2. **Test.** Add a test in `internal/keymap/keymap_test.go` that looks the key up (`m.Lookup(ctx, key)`, or `Machine.Feed` for a no-prefix key) and asserts the action, as `TestFilterBarAndGridFindDefaults` does. Add a conflict case if the key shadows something.
3. **Terminal handler.** Dispatch on `"my_action"` in `internal/ui`: in the `switch` of `Workspace.action` (`workspace.go`) for workspace-wide actions, in `App.run` or `runAppAction` (`app.go`, `connections.go`) for actions that work without a workspace, or in the focused tab's or sidebar's `Action(name)` method (`tab_table.go`, `tab_query.go`, `sidebar.go`) for pane-level actions. The quoted name must appear in the source.
4. **Web `SUPPORTED`.** Add `'my_action'` to the `SUPPORTED` set in `web/src/routes/Workspace.tsx`.
5. **Web handler.** Either a `case 'my_action':` in the `switch` of `act.current` in `Workspace.tsx` (workspace-wide), or answer the `webtuidb:action` window event in the component that owns the behaviour: a table in `keyActions` (`web/src/components/grid/DataTab.tsx`), `acts` (`web/src/components/query/QueryTab.tsx`), the sidebar, the filter bar, the console or the assistant tab. The handler calls `preventDefault()` on the event to claim it; if nobody claims it, the workspace tells the user "nothing to do here". The focused context for the lookup comes from the nearest `data-ctx` attribute (`sidebar`, `grid`, `editor`, `results`, `structure`, `filterbar`, `console`) around the focused element, so a new pane that should have its own bindings needs a `data-ctx`.
6. **Help listings.** Nothing to write: the TUI help dialog, `webtuidb -keys`, `GET /api/meta` and the web help all read the same table, and `make parity` proves they match. If the action should also be a toolbar button in the web, `[web] toolbar` accepts any action name (and `[general] hints` for the terminal status line).
7. **If it cannot work in one UI,** say so in the description, pick the closest key for that UI, and explain in the commit; do not leave the action out of one UI silently.
8. **Docs.** Regenerate `docs/data/keymap.json` (`docs/tools/export-meta.sh`, see [Documentation]({{< relref "documentation" >}})) so the shortcuts table is current, then run `make parity` and `go test ./internal/keymap`.

## Add a setting

1. **Struct.** Add the field, with a `toml:"my_setting"` tag, to the section struct in `internal/config/config.go` (`General`, `Editor`, `Table`, `CSV`, `Sidebar`, `Theme`, `Export`, `Web`, `Queries` or `Security`). `config.Load` rejects any key that is not in the structs (`unknown setting`), so this is also what makes the key legal.
2. **Default.** Set it in `Defaults()` in the same file.
3. **Validation.** If the value is constrained, add a check to `validate()`. For a choice, add the allowed values to the `enums` map (`"table.my_setting": {"a", "b"}`) and an entry in the list `validate()` walks, so a wrong value in the file is reported with the allowed ones.
4. **Doc line.** Add `"table.my_setting": "One line of help."` to `settingDocs` in `internal/config/setting_docs.go`. `TestSchemaDocsAndValues` fails for a setting without one.
5. **Schema.** Nothing to write: `config.Schema` reads the struct tags and gives each field its type (`string`, `bool`, `int`, `list` for `[]string`, `enum`), documentation and current value. A map-valued field and the secrets (`web.password`, `security.passcode`) are not offered. If the setting names a file or folder on the server and the browser must not change it, also add it to `pathSettings` in `internal/web/settings.go`.
6. **Parity entry.** Add a `ParityKey` to `config.Parity` in `internal/config/parity.go`, with `TUI` and `Web` true when the UI reads it, and a `Why` for each side that does not.
7. **Honour it in the terminal.** Read it from `a.d.Cfg.<Section>.<Field>` (or the workspace's copy) in `internal/ui`.
8. **Honour it in the web.** The web reads the sections `general`, `editor`, `table` and `sidebar` from `meta.config` (the structs are exported field by field, both as `PascalCase` and `camelCase`; `csv` as a struct): `cfg(meta, 'table', 'mySetting', default)` from `web/src/utils/config.ts`. Server-side behaviour is read from `s.cfg()` in `internal/web`. A `[web]` setting that only the browser uses is added to the `web` object in `meta()` in `internal/web/handlers.go`.
9. **Both settings editors need nothing.** The terminal settings dialog (`settingsDlg` in `internal/ui/settings.go`) and the web `SettingsDialog` both render from `config.Schema`, so a new field shows up in both with its doc line, and both save through `config.ApplyValues`, which validates the whole file before writing it.
10. Add the setting to the reference and run `make parity` and `go test ./internal/config`.

## Add a menu entry

1. Add an `Item{Label, Action, When, UI, Why}` to the right list in `internal/menus/menus.go`. `Action` is the keymap action when one exists (so the help can show the key) and otherwise a menu-only id. `When` is a `;`-separated list of predicates that must all hold: an object kind (`table`, `view`, `matview`, `function`, `procedure`, `sequence`, …), `tableish`, `definition`, `editable`, `marked`, `group`, or `cap:<Capability>`. Set `UI` to `tui` or `web` only when the other front end cannot do it, and give a `Why`.
2. Terminal: handle the action in the menu's pick callback or in the action switch (`internal/ui/menus.go` builds the pickers from `menus.Filter`). The action has to be dispatched as `case "…"` or `== "…"` somewhere in `internal/ui`.
3. Web: the menu reads `meta.menus.<list>` and runs the entry through `onPick` (for example `keyActions[it.action]` in `DataTab.tsx`, `colAct` for the column header, `onAction` in the sidebar). Implement the action there.
4. A **new list** also goes into `All()` at the bottom of `menus.go` (the web gets it through `/api/meta`), needs a `menus.<name>` reference in `web/src`, and a case in `TestParityMenusMatchLists` for the terminal.
5. If the entry opens a dialog, add the dialog pair (next section).

The predicates are evaluated by `menus.Item.Holds` in Go and by `holds()` in `web/src/utils/menus.ts`; change both if you add a predicate.

## Add a dialog pair

1. Build the dialog in both: a terminal overlay type or function in `internal/ui`, and a React component in `web/src/components`.
2. Add `{"Name shown in failures", "func (a *App) myDialog(", "export function MyDialog("}` to `Dialogs` in `internal/menus/dialogs.go`. Each of the two strings must occur verbatim in the sources of its side: use a function, type or method signature, or a title string that appears in only that dialog. Name options that were once missing in one UI (Cascade, Restart identity) in `Name`.
3. Run `make parity`.

## Deliberate differences

Not everything is identical, and each difference has to be written down where the test can see it:

- `config.Parity` entries with `TUI` or `Web` false and a `Why` (for example `web.password` has no meaning in the terminal; `sidebar.lazy_load` does not apply because the web loads one schema's objects at a time).
- Menu items with `UI: "web"` or `UI: "tui"` and a `Why`.
- Keymap descriptions that say so, for example `select_next_occurrence` ("terminal: one cursor, jumps; web: adds a caret").
- `// shortcut:` comments for limits in either front end; they are listed on [Contributing]({{< relref "contributing" >}}).
