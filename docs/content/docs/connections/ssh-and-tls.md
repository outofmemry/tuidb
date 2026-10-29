---
title: SSH tunnels and TLS
description: Reach a database through an SSH tunnel (keys, agent, passphrases, ProxyJump, ~/.ssh/config, the SSH log), encrypt it with TLS, route it through SOCKS5, or log in with an AWS RDS IAM token.
weight: 40
---

A connection can travel through an SSH tunnel or a SOCKS5 proxy, and can be encrypted with TLS. All of this is set per connection in the [connection form](../connection-form/) or in `connections.toml`.

{{< ui tui="connections-form-ssh-ssl" web="connections-form-ssh-ssl" >}}SSL mode `require` with a CA file, and an SSH tunnel through bastion.example.com with a key file.{{< /ui >}}

## SSH tunnels

With a tunnel, webtuidb logs in to the SSH host and opens the database connection from there, so **Host** and **Port** of the connection are the database as the SSH host sees it (often `127.0.0.1`, or a private address). Every driver supports it.

| Field | TOML key | Notes |
|---|---|---|
| **Over SSH** (terminal) / **SSH tunnel host** (web) | `[connection.ssh]` | switches the tunnel on |
| **SSH host** | `ssh.host` | name or address of the SSH server |
| **SSH port** | `ssh.port` | default 22 |
| **SSH user** | `ssh.user` | login on the SSH server |
| **SSH key file** | `ssh.key` | path to a private key; `~/` is expanded |
| **SSH password** | `ssh.password` | password for the SSH server |
| (file only) | `ssh.proxy_jump` | a bastion to go through first, see below |

```toml
[[connection]]
name = "Acme prod"
driver = "postgres"
host = "10.0.3.12"        # as seen from the bastion
user = "app"
database = "acme"
[connection.ssh]
host = "bastion.example.com"
user = "me"
key = "~/.ssh/id_ed25519"  # empty: ssh-agent
```

### How webtuidb authenticates

webtuidb offers, in this order, everything that is configured:

1. the key file, if **SSH key file** is set;
2. the password, if **SSH password** is set;
3. every key in your running ssh-agent (`SSH_AUTH_SOCK`).

With none of them it stops with `ssh: no key, password or running ssh-agent to authenticate with`.

**Encrypted keys.** When the key file has a passphrase, the terminal asks for it in an **SSH key passphrase** dialog, once per connect (a ProxyJump chain that uses the same key asks once). The web cannot prompt for it and fails with `ssh key ~/.ssh/id_ed25519 is encrypted (add it to ssh-agent)`: load the key into ssh-agent (`ssh-add`) and leave **SSH key file** empty, which works in both UIs.

**Host keys** are checked against `~/.ssh/known_hosts`. An unknown host is never trusted silently: the connect fails with `ssh: bastion.example.com is not in ~/.ssh/known_hosts; run ssh me@bastion.example.com once to trust it`. A changed host key fails with the SSH library's mismatch error. A missing `known_hosts` file is an error too (`ssh: cannot read ~/.ssh/known_hosts`).

The TCP connection to the SSH host times out after 15 seconds and the SSH handshake after 20; the whole connect, password prompts included, after 2 minutes.

### ProxyJump and ~/.ssh/config

To reach the SSH host through a bastion, set `proxy_jump` in the file:

```toml
[connection.ssh]
host = "db-gateway.internal"
user = "me"
proxy_jump = "me@bastion.example.com:22"   # several hops: "a,b"
```

When `proxy_jump` is empty, webtuidb uses the `ProxyJump` of the first `Host` block in `~/.ssh/config` that matches the SSH host (patterns with `*`, `?` and `!negation` work). `proxy_jump = "none"` turns that off.

The bastion is treated as its own host: its user comes from the `proxy_jump` spec, then its `~/.ssh/config` block (`User`), then your local login name; its address and port from `HostName` and `Port` in that block. It is offered its own `IdentityFile` when the block names one that exists, otherwise the connection's key file, plus the agent. The connection's SSH password is never sent to a bastion.

Only `ProxyJump`, `HostName`, `User`, `Port` and `IdentityFile` are read from `~/.ssh/config`, and the last four only for a bastion. For the SSH host itself, fill the fields in the form.

### The SSH log

To see why a tunnel does not come up, trace its setup to a file:

| | How |
|---|---|
| Terminal, at start | `webtuidb --ssh-log ~/ssh.log` |
| Terminal, while running | [Open Anything](../../workspace/) ({{< key "C-d p" >}}), **Enable SSH log**: new tunnels are traced to `~/.local/share/webtuidb/ssh.log`. The entry then reads **Disable SSH log** |
| Web | start the server with `webtuidb --frontend --ssh-log ~/ssh.log`. The **Enable SSH log** entry in the web's Open Anything only explains this: a page cannot switch it on |

{{< ui tui="connections-ssh-log" web="connections-ssh-log" >}}The SSH log entry in Open Anything.{{< /ui >}}

The log lists the dial (`ssh: dial host:22 as me`), the bastion hop (`via bastion …`), a failed handshake with its error, `connected to …`, and SOCKS5 use. It never contains passwords. The file is opened with mode 0600 and appended to.

## SSL modes

**SSL mode** (`ssl_mode`) decides whether the connection is encrypted and how much of the server's certificate is checked.

| Mode | Meaning |
|---|---|
| (empty) | the driver's default: PostgreSQL behaves like `prefer`, every other engine connects in plaintext |
| `disable` | plaintext |
| `prefer` | TLS when the server offers it, plaintext otherwise (see the caveat below) |
| `require` | always TLS; the certificate is not checked |
| `verify-ca` | TLS; the certificate must chain to a trusted CA, the host name is not checked |
| `verify-full` | TLS; chain and host name are checked |
| `verify-identity` | MySQL's name for `verify-full` |

**CA cert** (`ssl_ca`) replaces the system's trusted CAs with the PEM file you give (`ssl_ca …: no certificates found` if it holds none). **Client cert** and **Client key** (`ssl_cert`, `ssl_key`) send a client certificate. The three fields appear in the form once a mode other than `disable` is picked; TLS 1.2 is the minimum.

### Per engine

| Engine | Modes accepted | `prefer` |
|---|---|---|
| PostgreSQL, Redshift, Greenplum, Vertica | `disable`, `prefer`, `require`, `verify-ca`, `verify-full` (`verify-identity` is refused) | TLS if the server offers it, else plaintext |
| MySQL, MariaDB, SingleStore | all | TLS without checks, falling back to plaintext |
| Cassandra | all | encrypted, certificate not checked (no fallback) |
| ClickHouse | all | **plaintext** |
| Redis | all | **plaintext** |
| MongoDB | all | **plaintext** |
| DynamoDB | all; only for a custom `http(s)` endpoint, the AWS endpoint is always TLS | turns a custom endpoint into `https` |

{{< callout type="warning" >}}
ClickHouse, Redis and MongoDB have no opportunistic TLS: on them `ssl_mode = prefer` connects in plaintext, without a warning. Use `require` or a `verify-*` mode when the connection must be encrypted. For ClickHouse, point the port at the server's TLS port (usually 9440).
{{< /callout >}}

URLs can carry the mode in webtuidb's spelling (`sslmode=`) or the engine's own (`ssl-mode=`, `tls=`, `secure=`, `ssl=`), and `rediss://` means `require`. See [Connection URLs](../connection-urls/#query-options).

The [connection details](../connection-list/#connection-details) show the mode in use: `TLS: off` or `on (require)` in the terminal, the mode and `through an SSH tunnel` in the web.

## SOCKS5 proxy

**SOCKS5 proxy** (`socks_proxy = "127.0.0.1:1080"`) sends the database connection through a SOCKS5 proxy, such as `ssh -D 1080 bastion` or a corporate proxy. No proxy login is sent. When the connection also has an SSH tunnel, the tunnel wins and the proxy is not used. The connection details list it as `SOCKS:`.

## RDS IAM authentication

**AWS IAM auth** (`iam_auth = true`) logs in to an Amazon RDS or Aurora PostgreSQL, MySQL or MariaDB database with a short-lived IAM token instead of a password. The field is shown only for those three drivers; on any other driver the connect fails with `iam_auth is AWS RDS IAM authentication and is not available for <driver>`.

- **User** is the database user that has the `rds_iam` role (PostgreSQL) or the `AWSAuthenticationPlugin` (MySQL).
- Credentials come from the default AWS chain: environment variables, `~/.aws` shared config and SSO, or the instance role.
- The region comes from the option `region=` (in **Options**), else the AWS config. Without one: `iam_auth: no AWS region (set option region=... or AWS_REGION)`.
- A token lasts 15 minutes. webtuidb signs a fresh one for every new connection in the pool, so long sessions keep working.
- The token must not travel in clear: an empty SSL mode becomes `require`, and `disable` or `prefer` are refused with `iam_auth needs TLS: ssl_mode "prefer" would send the IAM token unencrypted (use require, verify-ca or verify-full)`.

```toml
[[connection]]
name = "Orders (RDS)"
driver = "postgres"
host = "orders.abc123.eu-west-1.rds.amazonaws.com"
user = "app_iam"
database = "orders"
iam_auth = true
ssl_mode = "verify-full"
ssl_ca = "/Users/me/certs/rds-global-bundle.pem"
[connection.options]
region = "eu-west-1"
```

## Details and limits

- `ssh.key` and `ssh.password` are dropped when connections are [imported from a file](../import-export-connections/), and a pasted `+ssh` URL keeps its `ssh_key` as written: check it before saving.
- There is no field for `proxy_jump`; set it in `connections.toml`. Edits in either form keep it.
- `ssl_ca`, `ssl_cert` and `ssl_key` are read as given: `~/` is not expanded in them (it is in `ssh.key`). Use absolute paths.
- A tunnel is opened for each workspace and closed with it; keep-alive pings keep idle tunnels from being dropped (see [Sessions and databases](../sessions-and-databases/#keep-alive)).
