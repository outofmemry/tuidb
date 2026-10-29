---
title: Safety
weight: 8
---

webtuidb sits between you and databases you may not want to break. Safe mode decides, per connection, whether a
statement is sent at once, after a confirmation or only with the database password. Destructive actions ask you
to type a name, read-only connections refuse writes, a passcode locks the app, and the web server only answers
the browser that holds its token. Every page covers the terminal and the browser.

{{< cards >}}
  {{< card link="safe-mode" title="Safe mode" subtitle="The five levels, what counts as read-only, the prompts and where they apply." >}}
  {{< card link="destructive-confirmations" title="Destructive confirmations" subtitle="Typed names, unsent changes, restore into a new database, read-only connections, bootstrap statements." >}}
  {{< card link="passcode-lock" title="Passcode lock" subtitle="-set-passcode, lock_after_mins and the lock screen." >}}
  {{< card link="web-security" title="Web security" subtitle="The token, password login, TLS, loopback listening, origin checks, Docker." >}}
  {{< card link="secrets-and-files" title="Secrets and files" subtitle="Plain-text passwords, password_cmd, encrypted exports, -bugreport, crash log, history." >}}
{{< /cards >}}
