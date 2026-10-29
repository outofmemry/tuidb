---
title: Connection form
description: Every field of the new and edit connection form, per driver, plus password commands, ask-every-time passwords, options, bootstrap SQL, testing and the checks connections.toml must pass.
weight: 20
---

The connection form creates a connection or changes a saved one. Both UIs write the same `[[connection]]` entry to `~/.config/webtuidb/connections.toml`; this page lists every field, what it does for each driver, and the TOML key it is saved under.

{{< ui tui="connections-form" web="connections-form" >}}Editing Shop (Postgres).{{< /ui >}}

## How to open it

| | Terminal (connection list) | Web (start page) |
|---|---|---|
| New connection | {{< key "n" >}} (driver from the highlighted engine tile) | **Add connection**, or one of the engine buttons |
| Edit | {{< key "e" >}}, or {{< key "m" >}} then **Edit…** | **⋯** then **Edit…**, or **Edit connection…** after a failed connect |
| From a URL | {{< key "i" >}}, paste, {{< key "Enter" >}} | paste into **Paste a connection URL**, **Continue** |

The terminal titles the dialog **New connection** or **Edit connection**; the web **New connection** or **Edit *name***.

### Keys in the terminal form

| Key | What it does |
|---|---|
| {{< key "Tab" >}} / {{< key "↓" >}} | next field |
| {{< key "S-Tab" >}} / {{< key "↑" >}} | previous field |
| {{< key "←" >}} {{< key "→" >}} / {{< key "Space" >}} | cycle a choice (Driver, Tag, Safe mode, SSL mode) or flip a checkbox |
| {{< key "Enter" >}} | save; a problem is shown in the form and the form stays open |
| {{< key "Esc" >}} | close; if you changed anything it asks **Discard changes to this connection?** |
| {{< key "C-t" >}} | test the connection with the values in the form, without saving |
| {{< key "C-o" >}} | pick a password command preset |

In the web form, {{< key "Esc" >}}, the backdrop, ✕ and **Cancel** all ask **Discard the changes to this connection?** when something changed. The web form has a **Test** button instead of {{< key "C-t" >}}: the browser opens a new tab on Ctrl+T before the page can see it.

## Fields

The terminal groups the fields under **Server**, **Workspace**, **Transport** and **After connecting**; the web lists them in one column. Fields that do not apply are hidden: the TLS certificate fields until an SSL mode other than `disable` is picked, the SSH fields until the tunnel is switched on, **AWS IAM auth** for drivers other than PostgreSQL, MySQL and MariaDB.

| Field (terminal / web) | TOML key | What it does |
|---|---|---|
| — / **Connection URL** | | web only: the whole connection as a URL, masked because it carries the password. It follows the fields as you type; typing or pasting a URL (or **Paste URL…**) and leaving the box fills the fields from it. **Show** / **Hide** unmask it |
| **Name** | `name` | required, unique; how the connection appears in lists, history and `webtuidb <name>` |
| **Driver** | `driver` | `postgres`, `mysql`, `mariadb`, `redshift`, `greenplum`, `vertica`, `singlestore`, `clickhouse`, `cassandra`, `dynamodb`, `redis` or `mongodb`. Changing it swaps the port for the new driver's default when the port was empty or another driver's default |
| **Host** | `host` | server name or address; a new connection starts with `127.0.0.1` |
| **Port** | `port` | 1-65535; empty means the driver's default (table below) |
| **User** | `user` | the database login |
| **Password** | `password` | stored in plain text in `connections.toml` (mode 0600). `ask` means prompt on every connect (below); the web has an **Ask every time** checkbox for it |
| **Password cmd** / **Password command** | `password_cmd` | a program whose output is the password (below) |
| **Database** | `database` | the database to open; empty is the server's default. Its meaning changes per driver (table below) |
| **Tag** | `tag` | `local`, `testing`, `development`, `staging`, `production`, or a `[[tag]]` of yours. See [Tags, colours and groups](../tags-colours-groups/) |
| **Status color** / **Color** | `color` | `#rrggbb`; colours the tag badge, the list stripe and the web accent. The web also takes a CSS colour name such as `teal` and saves its hex code |
| **Safe mode** | `safe_mode` | `silent`, `alert1`, `alert2`, `safe1` or `safe2`. The terminal pre-fills `[general] default_safe_mode` for a new connection; the web's **default** leaves the key out so the setting applies. See [Safe mode](../../safety/safe-mode/) |
| **Group** | `group` | the folder in the connection list; `A/B` nests. Leading and trailing `/` are trimmed |
| **Encoding** | `encoding` | client encoding override: MySQL / MariaDB / SingleStore `charset`; PostgreSQL accepts only UTF-8 (anything else is refused, because the client does not transcode). Letters, digits, `_` and `-` only |
| **SSL mode** | `ssl_mode` | empty (the driver's default), `disable`, `prefer`, `require`, `verify-ca`, `verify-full`, `verify-identity`. See [SSH tunnels and TLS](../ssh-and-tls/#ssl-modes) |
| **CA cert** | `ssl_ca` | PEM file of the certificate authority to trust |
| **Client cert** | `ssl_cert` | PEM client certificate, for servers that ask for one |
| **Client key** | `ssl_key` | its private key |
| **SOCKS5 proxy** | `socks_proxy` | `host:port` of a SOCKS5 proxy for the database connection; ignored when an SSH tunnel is set |
| **AWS IAM auth** | `iam_auth` | use an RDS IAM token instead of the password (PostgreSQL, MySQL, MariaDB). See [RDS IAM auth](../ssh-and-tls/#rds-iam-authentication) |
| **Read only** | `read_only` | refuse every statement that changes data on this connection. PostgreSQL and MySQL enforce it on the server too |
| **Over SSH** / **SSH tunnel host** | `[connection.ssh]` | the terminal has an **Over SSH** checkbox; the web shows the other SSH fields once **SSH tunnel host** has a value |
| **SSH host** | `ssh.host` | required when the tunnel is on (`ssh host is required when Over SSH is on`) |
| **SSH port** | `ssh.port` | default 22 |
| **SSH user** | `ssh.user` | the login on the SSH host |
| **SSH key file** | `ssh.key` | private key path, `~/` allowed; empty uses the password or ssh-agent |
| **SSH password** | `ssh.password` | password for the SSH host |
| **Options** | `[connection.options]` | driver extras as `key=value`, several separated by ` ;; ` (table below) |
| **Bootstrap SQL** | `bootstrap` | statements to run on every new connection, separated by ` ;; ` (below) |

Settings that have no field are kept as they are when you edit in either form: `driver_version`, `lazy_load`, `[connection.tools]` and `ssh.proxy_jump`. Edit them in the file.

### Per driver

| Driver | Default port | Database means | Notes |
|---|---|---|---|
| `postgres` | 5432 | the database | Options are passed as connection parameters, for example `application_name=webtuidb ;; connect_timeout=5` |
| `redshift` | 5439 | the database | the PostgreSQL driver with Redshift's capabilities (no indexes, triggers, materialized views or process list) |
| `greenplum` | 5432 | the database | PostgreSQL driver profile |
| `vertica` | 5433 | the database | PostgreSQL driver profile |
| `mysql`, `mariadb`, `singlestore` | 3306 | the database | **Encoding** sets the charset; options are not used |
| `clickhouse` | 9000 | the database | the native protocol (9000, or 9440 for its TLS port), not HTTP 8123. An empty user logs in as `default` |
| `cassandra` | 9042 | the keyspace | options `consistency` (default `LOCAL_QUORUM`), `local_dc`, `keyspace` (used when Database is empty), `timeout` (a duration such as `10s`), `protocol_version`; any other option is an error |
| `dynamodb` | none | the AWS region | see below |
| `redis` | 6379 | the database number (`0`, `1` …) | anything but a number is refused: `redis database must be a db index`. User is optional (Redis ACL); an empty host means `127.0.0.1` |
| `mongodb` | 27017 | the database | empty opens `test`. With a user set, `authSource` (default `admin`) names the authentication database. The client always connects directly to the one host, so `directConnection` is not needed; replica-set seed lists and `mongodb+srv://` are not supported |

**DynamoDB** reuses the fields:

- **Host** empty: the real AWS endpoint for the region. Host set: a custom endpoint such as DynamoDB Local, over `http` unless an SSL mode other than `disable` is set or the option `scheme=https` is given. The option `endpoint=https://host:port` overrides host and port.
- **User** / **Password**: access key id and secret access key. Both empty: the default AWS credential chain (environment, shared config, SSO, instance role). Option `profile=` picks a shared-config profile, `session_token=` adds temporary credentials.
- **Database**: the region, such as `us-east-1`; the option `region=` wins when both are set. A value that is not a region name is refused.

{{< callout type="info" >}}
A new connection's Host starts as `127.0.0.1` in both forms. For DynamoDB on AWS, clear it, otherwise webtuidb looks for a local endpoint at `http://127.0.0.1`.
{{< /callout >}}

## Password commands

`password_cmd` keeps the password out of the file: webtuidb runs the command each time it connects and uses what it prints, with trailing line breaks removed. When `password_cmd` is set, `password` is ignored.

The command runs directly, not through a shell: spaces separate arguments, single or double quotes group them, and pipes, `$VARIABLES` and `~` are not expanded. It has 30 seconds to finish. A failure stops the connect with `password_cmd for "<name>" failed: …`; an unbalanced quote with `unbalanced quote in …`.

{{< ui tui="connections-password-cmd" web="connections-password-cmd" >}}Password command presets: {{< key "C-o" >}} in the terminal form, **Presets…** in the web form.{{< /ui >}}

Pick a preset instead of typing the command; `<name>` becomes the connection's name.

| Preset | Command | How to store the secret |
|---|---|---|
| macOS Keychain | `security find-generic-password -s webtuidb -a <name> -w` | `security add-generic-password -s webtuidb -a <name> -w` |
| Linux secret-tool | `secret-tool lookup service webtuidb account <name>` | `secret-tool store --label=webtuidb service webtuidb account <name>` |
| pass | `pass show webtuidb/<name>` | `pass insert webtuidb/<name>` |
| 1Password | `op read "op://Private/<name>/password"` | needs the `op` CLI signed in |

The terminal shows the "how to store" line in the status bar after you pick; the web shows it under the field.

{{< callout type="warning" >}}
The web form shows **Password command** and **Bootstrap SQL**, but the server refuses to save a changed value in either: `"<name>": password_cmd and bootstrap can only be edited in connections.toml`. Both run programs or statements on the machine that runs webtuidb, so only the file (or the terminal form) may set them. Existing values are kept when you save other changes from the web.
{{< /callout >}}

## Ask for the password every time

`password = "ask"` (the web's **Ask every time** checkbox, or typing `ask` as the password in the terminal form) stores no password. Each connect, test and reconnect prompts for it: a **Password** dialog in the terminal, **Password for *name*** in the browser. In the terminal the prompt comes before the 30-second connect timeout starts, so typing slowly does not time out. Cancelling stops the connect.

{{< ui tui="connections-password-ask" web="connections-password-ask" >}}A connection with `password = "ask"` prompts on connect.{{< /ui >}}

An export without passwords keeps the `ask` marker, so the imported connection still prompts. A URL with `?password=ask` sets it too.

## Bootstrap SQL

`bootstrap` is a list of statements run whenever a new connection to the server is made, for example `SET search_path TO analytics, public ;; SET statement_timeout = '30s'`. Each statement is shown in the console log.

| Driver | When the statements run |
|---|---|
| PostgreSQL family, MySQL family | on every new physical connection in the pool |
| Redis | on every new connection, as Redis commands |
| ClickHouse | `SET name = value` becomes a client setting for every connection; any other statement runs once |
| Cassandra | `USE keyspace` picks the keyspace and `CONSISTENCY level` the default consistency; any other statement runs once |
| MongoDB, DynamoDB | once, after connecting, as editor commands |

A failing bootstrap statement makes the connect fail, with `bootstrap (<statement>): <error>`. See [Sessions and databases](../sessions-and-databases/#bootstrap-failures).

## Test the connection

{{< key "C-t" >}} in the terminal form, **Test** in the web form, or **Test** in the connection menu opens the connection with the values on screen, reports the server and closes it again. Nothing is saved.

{{< ui tui="connections-form-test" web="connections-form-test" >}}A working test reports the server version.{{< /ui >}}

The terminal says `Shop (Postgres): connection works (PostgreSQL 16.15)` in the status bar; the web shows **Connected: PostgreSQL 16.15** next to the buttons. A failure opens **Connection test failed: *name*** with the driver's error in the terminal; the web prints the error in red next to the buttons.

{{< ui tui="connections-form-test-failed" web="connections-form-test-failed" >}}The same test with the port changed to one nothing listens on.{{< /ui >}}

The web refuses to test a connection that has a password command or bootstrap statements (`password_cmd and bootstrap are not accepted in a test; save the connection and open it`), because the test request carries the whole connection and must not be able to run programs on the server.

## Saving and validation

**Save** ({{< key "Enter" >}} in the terminal) checks the form first. Messages you may see:

| Message | Cause |
|---|---|
| `connection needs a name` / **A name is required.** | empty Name |
| `a connection named "x" already exists` | the name is taken |
| `port must be 1-65535` | Port is not a number in range |
| `ssh port must be a number`, `ssh host is required when Over SSH is on` | SSH fields |
| `encoding "x" must be a plain charset name` | spaces or punctuation in Encoding |
| `unknown safe mode "x" (want silent\|alert1\|alert2\|safe1\|safe2)` | Safe mode |
| **Use a colour name like teal or a code like #7aa2f7.** | web: Color is neither |

### Checks on connections.toml

Both UIs refuse to start when `connections.toml` (or a file in a connection source folder) has a mistake that could make a connection less safe than you meant, and print the reason:

| Problem | Message |
|---|---|
| a misspelled key such as `ssl_mod` | `connections.toml: unknown setting "connection.ssl_mod"` (keys under `[connection.options]` are free-form and never rejected) |
| an unknown safe mode | `connection "a": safe_mode: unknown safe mode "alert3" (want silent\|alert1\|alert2\|safe1\|safe2)` |
| an unknown SSL mode | `connection "a": unknown ssl_mode "strict" (want disable\|prefer\|require\|verify-ca\|verify-full\|verify-identity)` |
| a repeated name | `duplicate connection name "a"` |
| a nameless entry | `connection needs a name` |
| a bad encoding or SSH port | `encoding "utf 8" must be a plain charset name`, `ssh port must be 1-65535` |
| invalid TOML | the parser's message with the file name |

A connection whose driver this build does not have (an old `sqlite` entry, or a typo such as `postgress`) does not stop the start: it is kept and saved as it is, a line `webtuidb: connection "a" is unusable: driver "sqlite" is not supported by this build` is printed, and connecting to it fails.

`connections.toml` holds passwords, so webtuidb only reads it with owner-only permissions: a file readable by others is changed to mode 0600 on load. If that fails (not your file, a read-only mount), the start is refused with `… is readable by other users and could not be fixed`, unless other users cannot read it anyway or its folder cannot be written; then it prints `warning: could not chmod … to 600 (…); continuing` and goes on. Files in connection source folders are never chmod-ed. Every save writes a new file with mode 0600 and renames it over the old one, so a crash leaves the old or the new file, never half of one. A save is refused if the file on disk no longer parses (`… changed on disk and is not valid TOML, refusing to overwrite it`).

## Details and limits

- In the terminal form, removing a `key=value` from **Options** does not delete that option from a saved connection: options you type are added or changed, but existing ones stay. Delete an option in the file.
- The terminal's **Status color** hint mentions colour names, but only a hex code works there: a name is saved with a `#` in front and is not a colour. Use `#rrggbb`, or the web form, which converts names.
- The web form checks a changed tool path under `[connection.tools]` (it must be an absolute path to a file named like the tool, such as `/opt/pg16/bin/pg_dump`). See [Backup and restore](../../import-export/).

## Settings

| Setting | Default | Effect |
|---|---|---|
| `[general] default_safe_mode` | `"silent"` | the safe mode of connections that set none |

See the [configuration reference](../../configuration/).
