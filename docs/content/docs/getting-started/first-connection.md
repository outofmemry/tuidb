---
title: First connection
description: Start webtuidb, save a connection or paste a URL, connect, and read the workspace that opens.
weight: 20
---

## The connection list

Run `webtuidb` with no arguments. The terminal opens the connection list: engine cards across the top filter the list, the table shows every saved connection with its driver, host, last-connected time and tag, and the quick-start panel on the right lists the keys that matter now.

{{< ui tui="connections-list" web="connections-list" >}}The connection list in the terminal and the start page in the browser.{{< /ui >}}

In the browser (`webtuidb --frontend --open`, see [Serving the web UI](../../web-ui/serving/)) the same list is the start page: a paste-a-URL field, one button per engine, and your connections grouped the same way.

## Adding a connection

Press {{< key "n" >}} in the terminal, or **Add connection** in the browser, and fill in the form: name, driver, host, port, user, password, database, and optionally a tag, colour, group, safe mode, SSL and SSH settings. {{< key "C-t" >}} tests the connection before you save it.

{{< ui tui="connections-form" web="connections-form" >}}The new-connection form.{{< /ui >}}

Every field, including `password_cmd` (run a command instead of storing the password), bootstrap statements and read-only mode, is described in [The connection form](../../connections/connection-form/).

## Pasting a URL

If you already have a URL, press {{< key "i" >}} in the terminal or paste it into the field at the top of the start page. You can also pass it on the command line:

```bash
webtuidb 'postgresql://user:pw@host/db?name=Prod&env=production'
```

`name=` becomes the connection name and `env=` its tag. A saved connection can be opened directly by name: `webtuidb pg-local`.

{{< ui tui="connections-import-url" web="connections-import-url" >}}Importing a URL.{{< /ui >}}

The table of ready-made URLs for DBngin, Docker and Homebrew servers is in [Connection URLs](../../connections/connection-urls/).

## The workspace

Press {{< key "Enter" >}} on a connection (or **Connect**). The workspace has a sidebar with the schema's objects, a tab bar, the pane with the current table or query, and a status line that shows the keys that apply right now.

{{< ui tui="workspace-layout" web="workspace-layout" >}}A fresh workspace on the demo shop database.{{< /ui >}}

Open a table with {{< key "Enter" >}} in the sidebar. From here the [tour](../tour/) walks through the everyday actions, and [Workspace](../../workspace/) explains every part of the frame.
