---
title: Users and privileges
description: Manage PostgreSQL roles and MySQL / MariaDB accounts - create, change the password, toggle attributes and global privileges, grant and revoke table and column privileges, set resource and connection limits, drop - with every change previewed as SQL and sent through safe mode, in the terminal and in the browser.
weight: 30
---

User management lists the server's accounts with their attributes and grants, and edits them. Every change is
planned as SQL, shown to you first, and sent through the normal commit path, so [safe mode](../../safety/),
read-only connections and the query history apply to it like to any other change.

{{< ui tui="tools-users-mysql" web="tools-users" >}}The accounts of the MySQL demo server: name and host, global privileges, and the per-object grants.{{< /ui >}}

## How to open it

| What | Terminal | Web |
| --- | --- | --- |
| Open user management | {{< key "C-d u" >}} (`users`) | {{< key "C-d u" >}} |
| From Open Anything | {{< key "C-d p" >}} → **User management** | {{< key "C-d p" >}} → **user management** |

It exists on PostgreSQL (and the PostgreSQL-based Greenplum and Redshift) and on MySQL, MariaDB and SingleStore.
Elsewhere both UIs say `user management is not supported by clickhouse`.

| | PostgreSQL | MySQL family |
| --- | --- | --- |
| An account is | a role (`pg_roles`, roles starting with `pg_` hidden) | `user@host` from `mysql.user` |
| Attributes | `SUPERUSER`, `CREATEDB`, `CREATEROLE`, `LOGIN`, `REPLICATION`, `BYPASSRLS` | global privileges (`GRANT … ON *.*`) |
| Grants listed | table, view, foreign-table and partitioned-table privileges from the ACLs, plus column-level grants; system schemas left out | parsed from `SHOW GRANTS` per account, including column grants; role grants skipped |
| Limits | `CONNECTION LIMIT` (`-1` = none) | `MAX_QUERIES_PER_HOUR`, `MAX_UPDATES_PER_HOUR`, `MAX_CONNECTIONS_PER_HOUR`, `MAX_USER_CONNECTIONS` |

## The user list

**Terminal.** A filterable picker titled `Users (6)`: the first line is **＋ New user…**, then one line per account
with its attributes and the number of grants. Type to filter, {{< key "Enter" >}} opens the account's menu,
{{< key "Esc" >}} closes.

**Web.** The **Users** dialog: a table with **User**, **Attributes** (PostgreSQL) or **Global privileges** (MySQL),
**Grants** (one line per object, column grants as `SELECT (email, city)`), and the action buttons **Password**,
**Attributes** / **Privileges**, **Limits**, **Grant**, **Revoke** and **Drop** on each row. **New user** sits under
the table. The actions open a form under the table; its **Preview** button plans the change.

{{< ui tui="tools-users-menu" >}}The menu of one account in the terminal.{{< /ui >}}

The terminal menu of an account, in order:

| Entry | Does |
| --- | --- |
| **Details** | name, attributes and every grant as text |
| **Change password…** | new password twice (they must match and not be empty) |
| **Attributes…** | the attribute (PostgreSQL) or global-privilege (MySQL) checklist |
| **Connection limit…** (PostgreSQL) / **Resource limits…** (MySQL) | the limits form |
| **Grant privileges…** | object, privileges and optional columns |
| **Revoke privileges…** | pick one of the account's grants, then edit the revoke form it fills in |
| **Drop user…** | type the name to confirm |

## Creating a user

| | Terminal | Web |
| --- | --- | --- |
| Start | **＋ New user…** | **New user** |
| Fields | **Name**, **Host** (MySQL, default `%`), **Password**, then the attribute checklist | **name**, **host** (MySQL, default `%`), **password**, and on PostgreSQL the attribute checkboxes |
| SQL (PostgreSQL) | `CREATE ROLE "name" WITH LOGIN CREATEDB PASSWORD '********'` | same |
| SQL (MySQL) | `CREATE USER 'name'@'%' IDENTIFIED BY '********'`, plus `GRANT SELECT, INSERT ON *.* TO 'name'@'%'` for the ticked global privileges | `CREATE USER …` only; set global privileges afterwards with **Privileges** |

The password is never shown: the Preview, the console and the history show `'********'`, while the statement
sent carries the real value (an `E'…'` string on PostgreSQL). On PostgreSQL the password is optional; MySQL
requires one (`password required`). MySQL refuses a backslash in a name, host or password
(`backslash is not allowed in names or passwords`), because the literal would be read differently under
`NO_BACKSLASH_ESCAPES`.

## Attributes and global privileges

{{< ui tui="tools-users-attrs" >}}The checklist in the terminal: Space ticks, `a` checks all, `A` unchecks all.{{< /ui >}}

The checklist offers `SUPERUSER`, `CREATEDB`, `CREATEROLE`, `LOGIN`, `REPLICATION`, `BYPASSRLS` on PostgreSQL and
`SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `DROP`, `ALTER`, `INDEX`, `ALL` on MySQL, with what the account has
already ticked.

| Key / control | Terminal | Web |
| --- | --- | --- |
| Toggle one | {{< key "Space" >}} or {{< key "x" >}} | the checkbox |
| Check all (again: uncheck all when everything is checked) | {{< key "a" >}} | **Check all** |
| Uncheck all | {{< key "A" >}} | **Uncheck all** |
| Move | {{< key "↑" >}} {{< key "↓" >}} or {{< key "k" >}} {{< key "j" >}} | |
| Apply | {{< key "Enter" >}} | **Preview** |
| Cancel | {{< key "Esc" >}} | **Cancel** |

What gets planned:

- **PostgreSQL** (both UIs): one `ALTER ROLE "name" WITH …` with each newly ticked flag and `NO<flag>` for each
  cleared one, for example `ALTER ROLE "report" WITH LOGIN NOCREATEDB`. Nothing changed: `no changes`.
- **MySQL, terminal**: `GRANT <new> ON *.* TO 'name'@'host'` for newly ticked privileges and
  `REVOKE <cleared> ON *.* FROM 'name'@'host'` for cleared ones.
- **MySQL, web**: **Privileges** starts with nothing ticked and only grants the ticked privileges globally; to take a
  global privilege away use **Revoke** with the object `*.*`.

`ALL` is written as `ALL PRIVILEGES` on MySQL.

## Granting and revoking

{{< ui tui="tools-users-grant" web="tools-users-grant" >}}A column-level grant: `SELECT` and `UPDATE` on two columns of `shop.customers`.{{< /ui >}}

| Field | Terminal | Web |
| --- | --- | --- |
| Object | **Object**: prefilled with the table under the sidebar cursor (PostgreSQL, `schema.table`) or `database.*` (MySQL) | the object box (placeholder `schema.table` or `db.table or db.*`) |
| Privileges | **Privileges**: comma separated, any case (prefilled `SELECT`) | checkboxes: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER` on PostgreSQL; the MySQL list above on MySQL; **Check all** / **Uncheck all** |
| Columns | **Columns**: optional, comma separated | the columns box, optional |

Objects:

- **PostgreSQL**: `schema.table`; a name without a dot means `public.table`. Always `ON TABLE`, so views and
  foreign tables work too; sequence, schema and database privileges are not offered.
- **MySQL**: `db.table`, `db.*` or `*.*`; a bare name means `name.*`.

With columns, every privilege applies to just those columns:
``GRANT SELECT (`email`, `city`), UPDATE (`email`, `city`) ON `shop`.`customers` TO 'webtuidb'@'%'``. Column
privileges exist for `SELECT`, `INSERT`, `UPDATE` and `REFERENCES`; the database refuses the others.

Privilege names are checked against a fixed list before anything is planned (they are SQL keywords and cannot be
quoted): on PostgreSQL the attributes above plus `SELECT INSERT UPDATE DELETE TRUNCATE REFERENCES TRIGGER USAGE
CREATE CONNECT TEMPORARY EXECUTE ALL`; on MySQL every static privilege (`ALTER ROUTINE`, `CREATE VIEW`,
`GRANT OPTION`, `LOCK TABLES`, `SHOW VIEW` …) and the MySQL 8 dynamic ones (`BACKUP_ADMIN`, `SYSTEM_USER` …). A word
outside the list fails with `bad keyword "FOO"` (PostgreSQL) or `bad privilege "FOO"` (MySQL).

**Revoke.** In the terminal, **Revoke privileges…** lists the account's grants; picking one fills the revoke form
with its object, privileges and columns, which you can trim before applying (`webtuidb@% has no table grants to revoke` when there are none). In the web, **Revoke** opens an empty form like **Grant**.

{{< callout type="warning" >}}
PostgreSQL objects are split at the **first** dot, so a schema whose name contains a dot is mis-read:
`my.schema.orders` becomes schema `my`, table `schema.orders`. Grant on such a schema in the SQL editor instead.
{{< /callout >}}

## Limits

{{< ui tui="tools-users-limits" >}}MySQL resource limits of one account.{{< /ui >}}

| | PostgreSQL | MySQL |
| --- | --- | --- |
| Terminal | **Connection limit…**: one number, `-1` = no limit (prefilled with the current value) | **Resource limits…**: **max queries per hour**, **max updates per hour**, **max connections per hour**, **max user connections** |
| Web | **Limits** → **Connection limit** | **Limits** → **Queries per hour**, **Updates per hour**, **Connections per hour**, **Simultaneous connections** |
| Values | a whole number, `-1` or more | whole numbers, `0` or more; `0` means unlimited, except **max user connections** where `0` means the server's `max_user_connections` |
| SQL | `ALTER ROLE "name" CONNECTION LIMIT 5` | `ALTER USER 'name'@'host' WITH MAX_QUERIES_PER_HOUR 0 MAX_UPDATES_PER_HOUR 0 MAX_CONNECTIONS_PER_HOUR 0 MAX_USER_CONNECTIONS 10` |

When the login cannot read the `max_*` columns of `mysql.user`, the limits are not shown as zeros (saving zeros
would wipe the real quotas): the terminal says `resource limits could not be read for name@host`, the web says
the limits cannot be changed here.

## Dropping a user

Both UIs ask you to type the account name (`type “report” to confirm` / `Type report to drop this role`); a wrong
name drops nothing. The plan is `DROP ROLE "name"` or `DROP USER 'name'@'host'`.

## Preview and safe mode

{{< ui tui="tools-users-preview" web="tools-users-preview" >}}The planned grant before it is sent: ``GRANT SELECT (`email`, `city`), UPDATE (`email`, `city`) ON `shop`.`customers` TO 'webtuidb'@'%'``. Nothing runs until you apply it.{{< /ui >}}

Every action ends in a preview of the planned statements. In the terminal it is `Preview: n statement(s)`:
{{< key "Enter" >}} applies, {{< key "Esc" >}} or {{< key "q" >}} closes. In the web it is the usual
**Send n statements?** dialog (`Send 1 statement?` for one) with **Send changes** and **Cancel**. Applying sends the statements through the same
path as a commit: the safe-mode confirmation or password, the read-only refusal, the console and the history. In
the terminal the user list reopens afterwards with the change in it.

## Details and limits

- There is no rename: create the new account, grant what it needs and drop the old one.
- In the web, an account with a long list of global privileges (`root`, or `webtuidb` on the demo server) makes the
  table wider than the dialog: scroll the table sideways to reach the action buttons at the right end of each row.
- PostgreSQL roles are listed with their table grants read from `pg_class` / `pg_attribute` ACLs (fast on big
  catalogs); default privileges, sequence and schema grants are not listed.
- MySQL accounts the current login may not inspect are listed without grants.
- In the web the PostgreSQL form is chosen by the driver name `postgres`; on Greenplum and Redshift connections
  the dialog shows the MySQL fields (host, global privileges), so use the terminal for those.
