---
title: Deep links
description: Addresses that open a workspace, a table with filters, or a saved query, how to copy them, how the access token travels with them, and what happens when the session behind a link is gone.
weight: 30
---

Every place you can be in the web UI has an address, so you can bookmark it, paste it into a chat, or open it in another browser tab. The terminal UI has no addresses; its closest equivalent is starting `webtuidb <connection>` or `webtuidb <url>`.

## The addresses

| Address | Opens |
|---|---|
| `/` | The start page. |
| `/s/<session>` | The workspace of an open session, with whatever tabs that browser tab had open. |
| `/s/<session>/t/<schema>/<table>` | A table tab in that workspace. |
| `/s/<session>/t/<schema>/<table>?filter=<json>` | The same table with filters applied. |
| `/s/<session>/q/<path>` | A saved query, in a new editor tab. |
| `/sql` | The read-only SQL file viewer (see [the start page](../browser-app/#the-start-page)). |

`<session>` is the id of a session on the server (16 hex characters, for example `78b27992b82e537e`); it is what the address bar shows after you connect.

**Tables.** `<schema>` and `<table>` are URL-encoded names. An engine without schemas (MySQL, Redis, MongoDB and others) uses `-` in the schema position: `/s/ab12…/t/-/orders`. The link opens the object the way a click in the sidebar does: if that table is already open in the pane it is shown, otherwise a new tab opens (or the current table tab is replaced when it has no unsent changes).

**Filters.** `filter` is a JSON filter set, the same shape the API uses:

```json
{"Filters":[{"Column":"status","Op":"=","Value":"paid","Enabled":true}],"Or":false}
```

`Or` true means a row matches when any enabled filter matches. `Enabled` defaults to true when left out. A link with a filter always opens a **new** tab, so a tab with unsent edits is never replaced. Once applied, the link's filters are that table's filters like any others: they are saved in `state.json` and come back the next time you open the table, in the browser and in the terminal. **Clear all** in the filter bar removes them. The `Op` is one of the engine's filter operators (`=`, `<>`, `Contains`, `IN`, `IS NULL`, `Has prefix` and so on; see [Filters](../../browsing-data/)). A `filter` value that is not JSON, is not an object with a `Filters` list, or whose entries lack a `Column` and an `Op` is ignored, and the table opens unfiltered: a hand-edited link never breaks the page.

**Saved queries.** `<path>` is the query's path under the queries folder with its slashes kept: `/s/ab12…/q/reports/top-customers.sql`. The editor tab opens with the query's text (it is not run) and remembers where it came from, so the tab's own **Copy link** gives this address back. If no saved query has that path, a toast says `No saved query <path>`.

## Copy a link

The tab menu has **Copy link** (right-click a tab, use its **▾**, or press the menu key or {{< key "S-F10" >}} on a focused tab). It copies an address made from the page's own origin:

| Tab | Link |
|---|---|
| A table tab | `/s/<session>/t/<schema>/<table>`, with `?filter=` holding the filters **applied right now**, not the ones the tab opened with. |
| An editor tab opened from a saved query | `/s/<session>/q/<path>`. |
| Any other tab | `/s/<session>`. |

A toast says **Copied the link**. The link carries no token, on purpose: a token in a pasted link would hand the API to whoever receives it.

{{< ui web="web-ui-copy-link" >}}
**Copy link** on a table tab with a filter applied.
{{< /ui >}}

The tab menu's other web-only entry, **Open workspace in a new browser tab**, opens `/s/<session>` in a new tab and signs it in; see below.

{{< ui web="web-ui-deeplink-table" >}}
A deep link with `status = paid`, opened in a signed-in tab: `orders` opens with the filter in the bar and 150 of 1,200 rows.
{{< /ui >}}

When you paste a link over the address of a browser tab that already shows that workspace, the page loads again and brings back its saved tabs; the tab that was in front before stays in front, and the linked table is added next to it. Click it to see it.

## How the token is carried

Every API call needs the access token. A link without it can show nothing, so the app keeps the token in the tab and moves it only through places that never reach a server log:

1. **The fragment.** The address printed at start ends with `#token=…`. On load the app copies the token into this browser tab's `sessionStorage` and rewrites the address without the fragment (the path and the `?filter=` stay).
2. **The hand-off.** When the app opens a new browser tab itself (**Open workspace in a new browser tab**), it writes the token to `localStorage` under `webtuidb-handoff` with a timestamp, opens the tab, and the new tab takes the entry out (and deletes it) if it is younger than 15 seconds and the tab has no token yet. The token is never in the new tab's address. If the browser blocks storage, the token is appended as `#token=` instead.
3. **Your hands.** In a browser tab that already runs the app, paste a deep link over the address: the tab's `sessionStorage` survives the navigation, so the link signs in. In any other tab, add the token yourself: `…/t/public/orders#token=<token>`.

A link opened in a fresh browser tab with none of these shows **This link needs the access token** and asks you to open the address `webtuidb --frontend` printed, then the link in the same browser.

{{< ui web="web-ui-token-needed" >}}
A deep link opened without a token.
{{< /ui >}}

With `[web] password` set, the sign-in cookie covers every tab of that browser: a deep link opens the sign-in page and, after signing in, the workspace. See [Serving the web UI](../serving/#sign-in-page-web-password).

## A link whose session is gone

The session id in a link names a live server-side session. It lasts until you disconnect, until the session is idle for an hour, or until the server restarts. Any browser that has the token can open a link to a live session, and then shares that session with every other tab that has it.

When the session no longer exists, the page does not guess. It shows **This session has ended**, the server's words (`no such session (it was closed or the server restarted)`) and a **Back to connections** button; the filter or saved-query path in the link is dropped. Connect again from the start page and open the table by hand, or copy a fresh link.

{{< ui web="web-ui-session-ended" >}}
A link to a session that no longer exists.
{{< /ui >}}

## See also

- [Serving the web UI](../serving/): the token, sessions and what the server keeps.
- [The browser app](../browser-app/): tabs and their menus.
