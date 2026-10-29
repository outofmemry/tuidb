---
title: Connections
weight: 2
---

A connection is a saved set of details for one database server: driver, host, login, TLS, SSH tunnel, tag, colour and safe mode. Both front ends read and write the same file, `~/.config/webtuidb/connections.toml`, so a connection you add in the terminal shows up in the browser and the other way round. These pages cover the connection list, every field of the connection form, URLs, tunnels and TLS, tags and groups, moving connections between machines, and what happens to a connection once it is open.

{{< cards >}}
  {{< card link="connection-list" title="Connection list" subtitle="The welcome screen and the start page: filter, search, sort, groups, row menu, copy as URL." >}}
  {{< card link="connection-form" title="Connection form" subtitle="Every field per driver, password commands, ask every time, options, bootstrap, test, file validation." >}}
  {{< card link="connection-urls" title="Connection URLs" subtitle="Import a URL, the schemes and query options, +ssh URLs, the CLI argument, local servers." >}}
  {{< card link="ssh-and-tls" title="SSH tunnels and TLS" subtitle="Keys, agent, passphrases, ProxyJump, the SSH log, SSL modes per engine, SOCKS5, RDS IAM." >}}
  {{< card link="tags-colours-groups" title="Tags, colours and groups" subtitle="Built-in and custom tags, the production cue, status colours, groups, connection sources." >}}
  {{< card link="import-export-connections" title="Export and import connections" subtitle="Passphrase-protected files, what is stripped on import, both UIs." >}}
  {{< card link="sessions-and-databases" title="Sessions and databases" subtitle="Open, switch and close connections, workspaces, reconnect, keep-alive, slow queries, bootstrap." >}}
{{< /cards >}}
