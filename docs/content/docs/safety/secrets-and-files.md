---
title: Secrets and files
description: Where webtuidb keeps passwords and other secrets, password_cmd and password = "ask", encrypted connection exports, what an import drops, -bugreport masking, the crash log and history.
weight: 50
---

webtuidb is a local program: it keeps its secrets in files under your home directory and sends nothing anywhere
on its own. This page lists every place a secret can live, how each file is protected, and the options that
keep passwords out of files entirely.

## Passwords in connections.toml

A connection's `password` is stored in `connections.toml` in plain text. The file is protected by its
permissions, not by encryption:

- webtuidb writes it atomically (a temporary file renamed into place) with mode `0600`, readable only by you.
- When it reads the file and finds it readable by group or others, it changes it to `0600`. If that fails on a
  file system where permissions cannot be set, it prints "warning: could not chmod … to 600 (…); continuing";
  otherwise it stops with "… is readable by other users and could not be fixed".
- Files from a `[[source]]` folder are shared files: webtuidb reads them but never changes their permissions or
  their content.
- An unknown key in `connections.toml` (a misspelled `password_cmd`, say) stops the load with
  `unknown setting "…"` rather than being ignored.

`config.toml` is set to `0600` the same way whenever it holds a secret: `[web] password`, an AI API key, an MCP
token, or a `[security] passcode` hash (a short passcode's hash can be guessed offline). If it cannot be changed,
webtuidb stops with "… holds secrets and is readable by other users; chmod 600 it".

## Keeping the password out of the file

### password_cmd

```toml
[[connection]]
name = "Shop prod replica"
password_cmd = "security find-generic-password -s webtuidb -a shop-prod -w"
```

On connect webtuidb runs the command and uses what it prints, without the trailing newline.

- The command runs directly, **not through a shell**: no pipes, `$VARS`, `~`, globbing or `;`. It is split on
  spaces; single or double quotes group an argument with spaces (`op read "op://Private/shop/password"`). An
  unbalanced quote fails with `password_cmd for "…": unbalanced quote in "…"`. A shared `connections.toml`
  therefore cannot run shell code on your machine. Write a small script and point `password_cmd` at it if you
  need a pipeline.
- It gets 30 seconds; a failure or a non-zero exit stops the connection with `password_cmd for "…" failed: …`.
- It runs again on every connect, so a rotated secret is picked up on the next connect (a session keeps the
  password it connected with).
- In the connection form the **Password cmd** field has presets ({{< key "C-o" >}} in the terminal); `<name>` in
  them becomes the connection name:

| Preset | Command | Store the password with |
|---|---|---|
| macOS Keychain | `security find-generic-password -s webtuidb -a <name> -w` | `security add-generic-password -s webtuidb -a <name> -w` |
| Linux secret-tool | `secret-tool lookup service webtuidb account <name>` | `secret-tool store --label=webtuidb service webtuidb account <name>` |
| pass | `pass show webtuidb/<name>` | `pass insert webtuidb/<name>` |
| 1Password | `op read "op://Private/<name>/password"` | needs the `op` CLI signed in |

`password_cmd` wins over `password` when both are set.

### password = "ask"

`password = "ask"` stores no password at all: webtuidb asks every time you connect. The terminal shows a
password prompt; the web shows "Password for *name*". The typed password is kept in memory for that session only.
It is also what the Safe Mode 1 and 2 [password prompts](../safe-mode/#password-safe-mode-1-and-2) compare with.

With `password_cmd` or `ask`, nothing secret is in `connections.toml`, so the file can be shared or committed.

## Exporting and importing connections

{{< ui tui="safety-export-connections" web="safety-export-connections" >}}Exporting connections with a passphrase. In the web, "Include passwords" is enabled only once a passphrase is typed.{{< /ui >}}

| | Terminal | Web |
|---|---|---|
| Export | `x` on the connection list: form "Export connections" with **What** (all, or a group), **File** (default `connections.wtdb`), **Passphrase**, **Include passwords** | right-click a connection, **Export connections…**: **Which**, **Passphrase**, **Include passwords (needs a passphrase)**; the file downloads as `connections.wtdb` |
| Import | `I` on the connection list: form "Import connections" with **File** and **Passphrase** | **Import connections…**: pick the file, **Passphrase** ("if the file is encrypted") |

What an export contains:

- With a passphrase, the file is encrypted with AES-256-GCM under a key derived from the passphrase with scrypt
  (N=32768, r=8, p=1) and a random salt. Without one it is plain TOML.
- **Include passwords** needs a passphrase: "include passwords needs a passphrase" (terminal), "passwords can only
  be exported under a passphrase" (web). Without it, stored passwords, `options.password` and `ssh.password` are
  blanked. `password = "ask"` is kept, so the importer is still asked. `password_cmd` is kept: it is a command,
  not a secret.
- The web never exports connections that come from a `[[source]]` folder ("Connections from a connection source
  are not exported: they live in their own files.").

What an import drops: a shared file must not run programs or read key files on your machine, so these fields are
removed from every imported connection:

| Field | Why |
|---|---|
| `password_cmd` | runs a program |
| `bootstrap` | runs statements on connect |
| `tools` | dump/restore tool paths run programs |
| `ssh.key`, `ssh.password` | point at or carry key material |

The terminal reports "imported N connections; left out for safety: *name*: password_cmd, …"; the web lists them
under "Dropped from the file, they could run programs or read keys on this machine:". Add them back by hand if
you trust the source. A name that already exists becomes "*name* (imported)" in the terminal and is skipped in the
web.

Import errors: "this file is encrypted: a passphrase is needed", "wrong passphrase or corrupt file", "encrypted
file is truncated". In the terminal they keep the form open.

## Bug reports: -bugreport

```sh
webtuidb -bugreport
```

{{< ui tui="safety-bugreport" >}}`-bugreport` with a passcode and a web password in config.toml: both are printed as "***".{{< /ui >}}

It prints the version, OS and architecture, Go version, `TERM`, the drivers your saved connections use, and your
`config.toml` with secrets masked, then exits. Nothing is sent; you paste it into an issue yourself.

- Every key whose name contains `password`, `passcode`, `api_key`, `secret` or `token`, and every key named
  `value` (MCP tokens), is replaced with `"***"`, at any depth: tables, inline tables, quoted keys, multi-line
  strings.
- Keys ending in `_cmd` (such as `api_key_cmd`) stay: they hold a command, not a secret.
- A password inside a URL becomes `user:***@host`.
- The file is decoded and re-encoded, so its comments are not shown. A file that does not parse is masked line by
  line instead, multi-line secrets included.
- `connections.toml` is not printed at all, only the list of drivers.

Read the output before you post it: a secret under an unusual key name (say `pw = …`) is not recognised.

## The crash log

If webtuidb panics it appends the error, the stack and the last 20 lines of the console log to
`~/.local/share/webtuidb/crash.log` (the data directory; `$XDG_DATA_HOME/webtuidb` when set) and says "crashed;
details were written to … (nothing was sent anywhere)". In the web server a panic fails only that request
("internal error: … (see …/crash.log)") and the server keeps running.

- The file is created with mode `0600` in a `0700` directory.
- Over 2 MB it is moved to `crash.log.1` and a new one starts.
- The console lines can contain SQL you ran, including values. Check the file before attaching it to an issue.

## History

Every statement you send is appended to the query history under `~/.local/share/webtuidb/history/` (files `0600`,
folder `0700`), as the preview showed it. Statements that set a user's password are planned with the password
replaced by `'********'` in what is shown and logged, on PostgreSQL and MySQL; a password you type yourself in the
SQL editor is logged as typed.

With `[general] log_original_values = true`, every committed `UPDATE` and `DELETE` from the grid also logs a
"pre-image": a CSV of the values before the change. It holds the changed rows' key values only, not the whole row
(every original value on a table without a primary key), as marked in the code. Turn it on when you need an audit
trail, and remember the history then holds data, not just statements.

## MCP tokens

MCP clients authenticate with tokens from `[[mcp.token]]` in `config.toml`, each with a `scope` of `read-only`,
`read-write` or `full`; an unknown scope stops the load. The tokens are secrets in `config.toml` (so the file is
kept at `0600`) and are masked by `-bugreport`. See [Automation](../../automation/) for scopes and the approval
sheet.

## Settings

| Setting | File | Meaning |
|---|---|---|
| `password`, `password_cmd` | `connections.toml` | literal password (or `"ask"`), or a command that prints it |
| `[general] log_original_values` | `config.toml` | log the original values of changed rows in history (default `false`) |
| `[security] passcode` | `config.toml` | see [Passcode lock](../passcode-lock/) |
| `[web] password` | `config.toml` | see [Web security](../web-security/) |

See the [configuration reference](../../configuration/) for file locations.
