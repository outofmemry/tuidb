---
title: Web UI
weight: 13
---

`webtuidb --frontend` serves a browser version of the same engine as the terminal UI. It shares your connections, history, saved queries and per-table state, enforces each connection's safe mode on the server, and runs the same prefix keys. These pages are about what is specific to the browser: serving it safely, the start page and workspace chrome, links, the phone layout, the HTTP API, and building the front end. Every feature page elsewhere in this manual shows both UIs, so they are not repeated here.

{{< cards >}}
  {{< card link="serving" title="Serving the web UI" subtitle="Flags, the access token, password and TLS, Docker, reverse proxies, what is shared with the terminal" >}}
  {{< card link="browser-app" title="The browser app" subtitle="Start page, workspace chrome, toolbar, keys in a browser, menus, drag and drop, themes" >}}
  {{< card link="deep-links" title="Deep links" subtitle="Addresses for tables and saved queries, Copy link, how the token travels" >}}
  {{< card link="mobile" title="On a phone" subtitle="The object drawer, stacked rows, full-screen dialogs" >}}
  {{< card link="api" title="HTTP API" subtitle="Every endpoint behind the app, for scripts (internal, may change)" >}}
  {{< card link="development" title="Development" subtitle="npm run dev, make web, the stack, tests and design rules" >}}
{{< /cards >}}
