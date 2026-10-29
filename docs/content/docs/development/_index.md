---
title: Development
weight: 16
---

How webtuidb is built, tested and kept consistent, for anyone who wants to build it from source, change it or send a fix. The terminal UI and the web UI share one Go engine, and most of what is here follows from that: one build produces both, one set of tests keeps them identical, and every change that touches a shortcut, a setting, a menu or a dialog goes into both.

{{< cards >}}
  {{< card link="building" title="Building from source" subtitle="Prerequisites, make targets, the embedded web app, versions, releases, Docker." >}}
  {{< card link="testing" title="Testing" subtitle="make test, the Docker test databases, smoke and end-to-end tests, fuzzing, CI." >}}
  {{< card link="parity" title="Terminal and web parity" subtitle="What make parity asserts; add an action, a setting, a menu entry or a dialog." >}}
  {{< card link="architecture" title="Architecture" subtitle="Package map and the request flow of a commit in each front end." >}}
  {{< card link="documentation" title="How these docs are built" subtitle="Hugo, the screenshot tooling, adding a page, search, publishing." >}}
  {{< card link="contributing" title="Contributing" subtitle="Commits, the shortcut: convention, review notes, reporting bugs." >}}
{{< /cards >}}
