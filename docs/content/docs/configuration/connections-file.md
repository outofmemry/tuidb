---
title: connections.toml
description: The saved-connections file, every field and what it means per driver, SSH, dump tools, validation, permissions, and extra connection folders.
weight: 30
---

Saved connections live in `~/.config/webtuidb/connections.toml` (or `$XDG_CONFIG_HOME/webtuidb/connections.toml`). Both UIs read and write the same file: the connection form in the terminal and in the browser edits it for you, and you can edit it by hand. This page describes the file; the forms are on [Connections](../../connections/).

`--config` does not move this file: it always sits in the config folder.

## Format

One `[[connection]]` table per connection. Sub-tables belong to the connection above them.

```toml
[[connection]]
name = "Acme prod"
driver = "postgres"
host = "db.internal"
port = 5432
user = "app"
password_cmd = "op read op://Private/acme-prod/password"
database = "acme"
tag = "production"
color = "#f7768e"
safe_mode = "alert2"
group = "Acme/Prod"
ssl_mode = "verify-full"
read_only = true
bootstrap = ["SET search_path TO app, public"]

[connection.options]
application_name = "webtuidb"

[connection.ssh]
host = "bastion.example.com"
user = "me"
key = "~/.ssh/id_ed25519"

[connection.tools]
pg_dump = "/opt/homebrew/opt/postgresql@16/bin/pg_dump"
```

## Fields

| Field | Type | Meaning |
|---|---|---|
| `name` | string, required | The name shown in the list and passed on the command line (`webtuidb "Acme prod"`). Must be unique in the file. |
| `driver` | string, required | `postgres`, `mysql`, `mariadb`, `redshift`, `greenplum`, `vertica`, `singlestore`, `clickhouse`, `cassandra`, `dynamodb`, `redis`, `mongodb`. |
| `host` | string | Server host name or IP. |
| `port` | int | Server port; omitted or `0` = the driver's default (see below). |
| `user` | string | Login name. |
| `password` | string | **Stored in plain text.** The special value `"ask"` prompts for the password on every connect and never stores it. |
| `password_cmd` | string | A command whose standard output (trailing newline removed) is the password. When set, it wins over `password`. |
| `database` | string | The database to open; its meaning depends on the driver (see below). |
| `tag` | string | `local`, `testing`, `development`, `staging`, `production`, or a tag of your own (`[[tag]]` in `config.toml`). |
| `color` | string | `#rrggbb`. The connection's colour; in the web it is the accent of the whole workspace. Wins over the tag's colour. |
| `safe_mode` | string | `silent`, `alert1`, `alert2`, `safe1`, `safe2`; empty = `[general] default_safe_mode`. See [Safety](../../safety/). |
| `group` | string | Folder in the connection list; `/` nests (`"Acme/Prod"`). |
| `read_only` | bool | Refuse every statement that is not read-only on this connection. |
| `ssl_mode` | string | `disable`, `prefer`, `require`, `verify-ca`, `verify-full`, `verify-identity`. |
| `ssl_cert`, `ssl_key`, `ssl_ca` | string | Paths of a client certificate, its key, and the CA bundle to verify the server against. |
| `bootstrap` | list | Statements run right after connecting (for example `SET search_path …`). A failure shows **Bootstrap command failed (the connection is still open)**. |
| `encoding` | string | Client encoding: MySQL / MariaDB `charset`. PostgreSQL connections accept only UTF-8 (`encoding "latin1" is not supported by PostgreSQL connections (only UTF-8; leave it empty)`). Letters, digits, `-` and `_` only. |
| `socks_proxy` | string | `host:port` of a SOCKS5 proxy for the database connection. |
| `iam_auth` | bool | Use an AWS RDS IAM token instead of the password (PostgreSQL, MySQL, MariaDB). See below. |
| `driver_version`, `lazy_load` | string, bool | Kept from a TablePlus URL (`driverVersion`, `lazyload`) so it can be copied back out; webtuidb itself does not use them. |
| `[connection.options]` | table of strings | Driver-specific extras (below). |
| `[connection.ssh]` | table | An SSH tunnel (below). |
| `[connection.tools]` | table of strings | Paths of the dump and restore tools for this connection (below). |

### `password_cmd`

The command is run directly, not through a shell, so a shared `connections.toml` cannot run shell code: quotes group arguments, but pipes, `$VARIABLES` and `~` do not work. It has 30 seconds. The connection forms offer ready-made commands:

| Store | `password_cmd` |
|---|---|
| macOS Keychain | `security find-generic-password -s webtuidb -a <name> -w` |
| Linux secret-tool | `secret-tool lookup service webtuidb account <name>` |
| pass | `pass show webtuidb/<name>` |
| 1Password | `op read "op://Private/<name>/password"` (needs the `op` CLI signed in) |

### Per driver

| Driver | Default port | `database` means | Notes |
|---|---|---|---|
| `postgres` | 5432 | database name | `ssl_mode` accepts `disable`, `prefer`, `require`, `verify-ca`, `verify-full` (not `verify-identity`); empty = libpq's default `prefer`. Every `[connection.options]` entry is passed to the server as a connection parameter (`application_name`, `options`, …). |
| `redshift`, `greenplum`, `vertica` | 5439, 5432, 5433 | database name | Capability profiles of the PostgreSQL driver. |
| `mysql`, `mariadb`, `singlestore` | 3306 | database name | `encoding` sets the client charset. |
| `clickhouse` | 9000 | database name | native protocol port. |
| `cassandra` | 9042 | keyspace | |
| `redis` | 6379 | db index (`0`, `3`…) | Anything else is refused: `redis database must be a db index, got "x"`. |
| `mongodb` | 27017 | database name | Option `authSource` (default `admin`) is the database the user is defined in. |
| `dynamodb` | none | AWS region (when option `region` is not set) | `host` (and `port`) point at a custom endpoint such as DynamoDB Local; empty = the AWS endpoint of the region. `user` / `password` are the access key id and secret; give both or neither (neither = the default AWS credential chain). Options: `region`, `endpoint` (a full `http(s)://host[:port]`, no credentials in it), `scheme` (`http` or `https` for `host`; default `https` when `ssl_mode` is set and not `disable`, else `http`), `profile` (a shared-config profile), `session_token`. |

### `iam_auth`

The password is replaced by a 15-minute RDS IAM token built from your AWS credentials (the default chain). The region is option `region`, else the AWS default region; without either: `iam_auth: no AWS region (set option region=... or AWS_REGION)`. The token must not travel unencrypted, so on PostgreSQL an empty `ssl_mode` becomes `require`, and `disable` or `prefer` are refused. Other drivers: `iam_auth is AWS RDS IAM authentication and is not available for <driver>`.

### `[connection.ssh]`

| Field | Meaning |
|---|---|
| `host` | The SSH server (bastion). |
| `port` | SSH port; empty = 22. Must be 1–65535. |
| `user` | SSH login. |
| `key` | Private key file; empty = use your ssh-agent. |
| `password` | SSH password (plain text), when not using a key. |
| `proxy_jump` | A `user@host:port` hop before `host`; empty = taken from `~/.ssh/config` when it has one. |

`webtuidb --ssh-log <file>` writes a debug trace of the tunnel.

### `[connection.tools]`

The command-line tools backup and restore run, by name: `pg_dump`, `pg_restore`, `psql`, `mysqldump`, `mysql`, `mariadb-dump`, `mariadb`, `clickhouse-client`, `mongodump`, `mongorestore`, `redis-cli`. An empty or missing entry means the tool is looked up on `PATH`. The browser may set a path only when it is absolute, has no `..`, and its file name is the tool's name or `<name>-<version>` (`pg_dump-16`). `tools = {}` means no overrides.

## Validation

The file is checked every time it is read. Any of these stops the program (or, in the browser, keeps the last good list and shows the error):

- `connection needs a name`
- `<path>: duplicate connection name "x"`
- `connection "x": unknown driver "y" (want postgres|mysql|…)`
- `connection "x": safe_mode: …`
- `connection "x": unknown ssl_mode "y" (want disable|prefer|require|verify-ca|verify-full|verify-identity)`
- `connection "x": encoding "y" must be a plain charset name`
- `connection "x": ssh port must be 1-65535`
- `<path>: unknown setting "connection.ssl_mod"`: a misspelled field is an error, never silently dropped. Only keys under `[connection.options]` are free-form.

A connection whose driver this build no longer has (for example an old `sqlite` entry) does not break the file: it is kept and saved as it is, shown as unusable, and the start prints `webtuidb: connection "x" is unusable: driver "sqlite" is not supported by this build`.

## Permissions and saving

{{< callout type="warning" >}}
Passwords in this file are stored in plain text. Prefer `password_cmd` or `password = "ask"`.
{{< /callout >}}

- The file is only read with owner-only permissions: if group or others can read it, webtuidb changes it to `0600`. If that fails (not the owner, read-only mount), it continues with a warning only when nobody else could read or replace the file anyway; otherwise it stops with `<path> is readable by other users and could not be fixed`.
- Saving writes a temporary file in the same folder with mode `0600`, flushes it to disk and renames it over the old one, so a crash leaves either the old file or the new one. The folder is created with mode `0700`.
- A save takes a lock on `connections.toml.lock`, so a terminal UI and a web server saving at the same moment do not overwrite each other. Connections another process added since this one last read the file are kept; ones this process deleted stay deleted. If the file on disk was changed into invalid TOML, the save is refused: `<path> changed on disk and is not valid TOML, refusing to overwrite it`.
- Saving re-encodes the file: comments are not kept.

## Reload on change

The web server checks the modification time and size of `connections.toml` and of every file in the `[[source]]` folders before it answers a request that needs the list, and re-reads them when they changed: an edit by hand or by the terminal UI shows up on the next page load. If the changed file does not parse, the server keeps the list it had and refuses to save until the file is fixed: `connections.toml does not parse, so nothing can be saved until it is fixed: …`. A browser tab that loaded the list before it changed elsewhere gets `the connection list changed elsewhere; reload it and retry` when it saves.

The terminal UI reads the file at start. Connections you add by hand while it runs appear after a restart; its own saves never remove them.

## Extra connection folders (`[[source]]`)

`config.toml` can point at folders of extra connection files, for example a team folder in git:

```toml
[[source]]
path = "~/work/infra/db-connections"
```

Every `*.toml` file in each folder is read in name order, in the `connections.toml` format. These connections:

- are listed with the others, and remember which file they came from;
- are read-only: both UIs never write to source files, and webtuidb never changes their permissions;
- lose to `connections.toml`: a name that already exists there (or in an earlier source file) is skipped.

An error in a source file stops the load with `source <file>: <error>`.

## Sharing connections

The connection list can export a portable file and import one. A file exported with a passphrase starts with `WTDB1` and is sealed with AES-256-GCM under a scrypt key of the passphrase; without a passphrase it is plain `connections.toml` format. Passwords are blanked unless you choose to include them (`password = "ask"` is kept). On import, `password_cmd`, `bootstrap`, `tools`, `ssh.key` and `ssh.password` are dropped unless you allow commands, because a shared file must not run programs or pick key files on your machine. Details: [Import and export connections](../../connections/import-export-connections/).
