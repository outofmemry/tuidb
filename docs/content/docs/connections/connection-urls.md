---
title: Connection URLs
description: Import a connection from a URL, the schemes and query options webtuidb reads, +ssh URLs, the command-line argument, copy as URL, and URLs for local servers.
weight: 30
---

A connection URL such as `postgresql://app:secret@db.internal:5432/shop?name=Shop&env=production` carries a whole connection in one line. You can paste one to create a connection, pass one on the command line to connect without saving, and copy any saved connection back out as a URL.

{{< ui tui="connections-import-url" web="connections-import-url" >}}Pasting a URL: {{< key "i" >}} on the welcome screen, or the paste box on the start page.{{< /ui >}}

## How to import a URL

| | Terminal | Web |
|---|---|---|
| Open | {{< key "i" >}} on the connection list, or **Import URL…** in the connection menu ({{< key "m" >}}) | the **Paste a connection URL** box on the start page, or **Import URL…** in a row's **⋯** menu (focuses the box) |
| Confirm | {{< key "Enter" >}} | {{< key "Enter" >}} or **Continue** |
| Inside the form | | the **Connection URL** field, or **Paste URL…** |

The URL is parsed and the [connection form](../connection-form/) opens filled in. Nothing is saved until you save the form, so you can fix the name, add a tag or test first.

{{< ui tui="connections-import-url-form" web="connections-import-url-form" >}}The form after importing `postgresql://webtuidb:webtuidb@127.0.0.1:55432/shop?name=Shop%20reporting&env=staging&sslmode=disable&application_name=webtuidb`: the name, tag and SSL mode come from the query, `application_name` lands in Options.{{< /ui >}}

In the web form, the **Connection URL** field stays in step with the fields. When you replace it and leave the field (or press {{< key "Enter" >}}), the driver, host, port, user, password, database, SSL mode, options and SSH tunnel come from the URL; the name you already typed is kept, and the tag and colour change only when the URL names them.

## Schemes

| Scheme | Driver |
|---|---|
| `postgresql://`, `postgres://` | `postgres` |
| `redshift://`, `greenplum://`, `vertica://` | the PostgreSQL profiles |
| `mysql://`, `mariadb://`, `singlestore://` | MySQL family |
| `clickhouse://` | `clickhouse` (native protocol port) |
| `cassandra://` | `cassandra`; the path is the keyspace |
| `dynamodb://` | `dynamodb`; `dynamodb://KEY:SECRET@host:port/?region=us-east-1`, the path is the region |
| `redis://`, `rediss://` | `redis`; the path is the database number (`redis://host/3`). `rediss://` turns on TLS (`ssl_mode = require`) |
| `mongodb://` | `mongodb` |

Add `+ssh` to any of them (`postgresql+ssh://`) to describe an SSH tunnel in the query (below). `mongodb+srv://` is refused: DNS seed lists are not supported, use `mongodb://host:port`. Any other scheme gives `unsupported URL scheme "ftp"`; the web adds **That doesn't look like a database URL. It should start with postgresql://, mysql://, mongodb://, redis://…**

The parts of the URL map to fields: `user:password@` to User and Password, `host:port` to Host and Port (no port means the driver's default), the path to Database. Percent-encode special characters in the password (`@` is `%40`, `%` is `%25`). A URL the parser cannot read is reported as `bad URL (not shown: it may contain a password; percent-encode special characters such as % @ in it)`: error messages never repeat the URL, because it may hold a password.

When the URL has no `name=`, the connection is named `host/database`, or `host` without a database.

## Query options

| Option | Effect |
|---|---|
| `name=` | the connection name |
| `env=` | the tag (`local`, `testing`, `development`, `staging`, `production`, or your own) |
| `statusColor=` | the status colour, as hex with or without `#` (`statusColor=f7768e`) |
| `password=ask` | prompt for the password on every connect. Any other `password=` value is ignored and never stored |
| `sslmode=` | the SSL mode: `disable`, `prefer`, `require`, `verify-ca`, `verify-full`, `verify-identity` |
| `tLSMode=` | the same, TablePlus's spelling |
| `ssl-mode=` | MySQL / MariaDB spelling: `DISABLED`, `PREFERRED`, `REQUIRED`, `VERIFY_CA`, `VERIFY_IDENTITY` |
| `tls=` | MySQL / MariaDB: `true` (require), `false` (disable), `skip-verify` (require), `preferred` (prefer). MongoDB: `true` / `false` |
| `ssl=` | MongoDB: `true` / `false` |
| `secure=` | ClickHouse: `true` / `false` |
| `driverVersion=`, `lazyload=true` | stored as `driver_version` and `lazy_load`; webtuidb keeps them so URLs from TablePlus round-trip, but does not act on them |
| `schema=`, `table=`, `column=`, `operation=`, `value=`, `condition=` | open a table right after connecting (command line only, below) |
| `ssh_host=`, `ssh_user=`, `ssh_port=`, `ssh_key=`, `usePrivateKey=` | the SSH tunnel of a `+ssh` URL |
| anything else | kept in the connection's Options (`[connection.options]`), for example `authSource=admin` for MongoDB or `application_name=` for PostgreSQL |

A TLS option with a value webtuidb does not know is an error (`unknown tls value "maybe" (want true|false)`, `unknown ssl mode …`) rather than a silent fallback to plaintext.

### +ssh URLs

```text
postgresql+ssh://app@10.0.0.5/shop?ssh_host=bastion.example.com&ssh_user=me&ssh_port=2222&ssh_key=~/.ssh/id_ed25519&usePrivateKey=true
```

`ssh_host` is required (`a +ssh URL needs ssh_host (and a network driver)`); `ssh_port` defaults to 22. `usePrivateKey=false` (or `0`) drops `ssh_key`, for password or agent authentication. Other `ssh…` parameters, such as an SSH password, are dropped and never stored. The database host and port are as seen from the SSH host. See [SSH tunnels](../ssh-and-tls/).

{{< callout type="info" >}}
`ssh_key` from a pasted URL is used as written. Check the path before you save a URL someone else gave you.
{{< /callout >}}

## On the command line

```sh
webtuidb 'postgresql://webtuidb:webtuidb@127.0.0.1:55432/shop?name=Scratch'
```

The terminal UI connects straight away. The connection is kept in memory only and is not saved to `connections.toml`. Quote the URL so the shell leaves `&` and `?` alone.

The argument is first looked up as a saved connection name; a URL is recognised by `://`. Anything else fails with `no saved connection named "x" (and it is not a URL)`.

**A URL with the name of a saved connection** starts from that connection and lays every part the URL supplies over it: driver, host, port, user, database, SSL mode, tag, driver version, `lazyload`, the SSH tunnel and the table to open replace the saved values, options are merged, and a password in the URL replaces both the saved password and `password_cmd`. Settings the URL does not mention (safe mode, colour, group, read only, bootstrap) stay. The saved entry is not changed. For example, to open the production replica in another database for one session:

```sh
webtuidb 'postgresql://127.0.0.1:55432/analytics?name=Shop%20prod%20replica'
```

### Open a table from the URL

`table=` (with an optional `schema=`) opens that table as soon as the connection is up; `column=`, `operation=` and `value=` add a filter, and `condition=or` joins filters with OR. `operation` accepts the filter bar's operators (`=`, `<>`, `LIKE`, `IN`, `IS NULL` …) and the aliases `eq`, `ne`, `gt`, `lt`, `gte`, `lte`, `contains`, `like`, `in`, `isnull`, `isnotnull`, `startswith`, `endswith`; the default is `=`. Without a schema the connection's current schema is used, and the sidebar switches to the URL's schema.

```sh
webtuidb 'postgresql://webtuidb:webtuidb@127.0.0.1:55432/shop?name=Shop%20(Postgres)&table=orders&column=status&operation=eq&value=paid'
```

The web UI takes no URL argument (`--frontend` refuses positional arguments); it has its own [deep links](../../web-ui/) to a table or a saved query.

## Copy as URL

{{< key "u" >}} on the connection list or **Copy URL** in the connection menu copies a saved connection as a URL without the password; {{< key "U" >}} or **Copy URL with password** includes it after a confirmation. See [the connection list](../connection-list/#copy-as-url). For Shop (Postgres) without the password:

```text
postgresql://webtuidb@127.0.0.1:55432/shop?env=development&name=Shop+%28Postgres%29&statusColor=7aa2f7
```

The URL uses `postgresql://` for PostgreSQL and the driver name otherwise, adds `+ssh` and the `ssh_host`, `ssh_user`, `ssh_port`, `ssh_key` and `usePrivateKey=true` options for a tunnel, `sslmode=` in webtuidb's spelling, every option, `name`, `driverVersion`, `lazyload`, `env` and `statusColor`. An IPv6 host is written in brackets. The SSH password, `password_cmd`, bootstrap, group, safe mode and read only are not part of a URL; use [export](../import-export-connections/) to move those.

## Local servers

Import a local server with {{< key "i" >}} (or the web paste box), or pass the URL as the argument. These are the default ports; use the one your tool shows for the instance.

| Server | URL |
|---|---|
| DBngin PostgreSQL | `postgresql://postgres@127.0.0.1:5432/postgres` |
| DBngin MySQL / MariaDB | `mysql://root@127.0.0.1:3306/` |
| DBngin Redis | `redis://127.0.0.1:6379/0` |
| Docker `postgres` (`-e POSTGRES_PASSWORD=pw -p 5432:5432`) | `postgresql://postgres:pw@127.0.0.1:5432/postgres` |
| Docker `mysql` (`-e MYSQL_ROOT_PASSWORD=pw -p 3306:3306`) | `mysql://root:pw@127.0.0.1:3306/` |
| Docker `mongo` | `mongodb://127.0.0.1:27017/` |
| Homebrew `postgresql@16` (`brew services start`) | `postgresql://$USER@127.0.0.1:5432/postgres` |
| Homebrew `mysql` / `mariadb` | `mysql://root@127.0.0.1:3306/` |
| Homebrew `redis` | `redis://127.0.0.1:6379/0` |

Add `?name=Local&env=local` to name and tag the connection. A Unix socket is not needed: `127.0.0.1` works for all of them.

## Details and limits

- TablePlus's `safeModeLevel=` is not read: it ends up in Options like any unknown key, and for PostgreSQL that makes the server reject the connection as an unknown parameter. Set the safe mode in the form after importing.
- Only one host per URL: MongoDB replica-set seed lists and Cassandra contact-point lists are not supported.
