---
title: Contributing
description: What a change to webtuidb should look like, how commits are written, the shortcut comment convention for deliberate limits, review notes and how to report a bug.
weight: 60
---

The repository has no `CONTRIBUTING` file and no pull request or issue templates. What follows is what `CLAUDE.md`, the CI workflow and the Git history establish; the project is small and these are the habits that keep it consistent.

## Before you open a pull request

CI runs on every push and pull request and has to pass (the steps are on [Testing]({{< relref "testing" >}})). Run the same things locally:

```sh
make test          # go vet + go test
make parity        # terminal and web agree
make web-test      # only if you touched web/ (also type-checks it)
gofmt -l .         # must print nothing
go test -race ./...
make up && make smoke   # if you touched a driver, the planner, or anything the UIs do to a database
```

A change comes with a test. Pure logic gets a unit test next to it; a behaviour of the terminal UI gets a test through the fake-session `harness` in `internal/ui`; a web endpoint gets one through `newEnv` in `internal/web`; web logic that is a pure module gets a `web/tests/*.test.mjs`. A bug fix starts with the test that fails.

## The rules of the codebase

- **One engine, two front ends.** Database logic belongs in `internal/db`, `internal/store`, `internal/config` and `internal/safemode`, not in `internal/ui` or `internal/web`. If both front ends need something, it goes in the engine and each front end calls it.
- **Shortcuts apply to both UIs.** Any change that mentions a shortcut, key binding, keymap, hotkey, prefix key or "when I press X" is made in the terminal and the web in the same change, and the two help listings stay identical. The step list is on [Terminal and web parity]({{< relref "parity" >}}); the same page covers settings, menu entries and dialogs.
- **Gate on capabilities, not on driver names.** A front end hides or refuses a feature by reading `db.Capabilities`. A new capability that a UI must gate on is added to the capability tests of both front ends (`internal/ui/parity_test.go` and `internal/web/parity_test.go`).
- **Safe mode is enforced on the server side.** The web server re-checks the gate in `apply` and `exec`, and only runs statements it planned. Do not add a path that sends client-supplied SQL around the gate or the connection's read-only flag.
- **Anything that loses work asks first** (closing or disconnecting with unsent changes, a commit on a table with no primary key, dropping an object by typing its name). Failures say what to check in one sentence and keep the driver's text behind "Technical details" (`web/DESIGN.md`).
- **Secrets stay out of logs, history and bug reports.** Statement displays hide passwords, the history stores what the user ran, and `-bugreport` masks every secret-looking key and URL password. Nothing is sent anywhere automatically; the network calls are the ones you configure (connections, the assistant, MCP), and `-check-update` runs only when asked.
- **Name files by what they do**, not by the milestone that produced them. The tree was reorganised by feature (`Reorganise the tree by feature: no more phase-named files`), and a new file such as `grid_sort.go` or `query_params.go` says what is in it.
- **Web styling** follows `web/DESIGN.md`: Tailwind utilities on the element, the palette tokens in `web/src/styles.css`, one popover style through `Layer`, visible keyboard focus, reduced motion respected, usable at phone width.
- **Keep the dev proxy honest.** `web/vite.config.ts` must keep `changeOrigin: false`, because the server checks `Origin` against `Host`.
- **Documentation.** A user-visible change updates the page that describes it, in both UIs, with the screenshots it changes ([Documentation]({{< relref "documentation" >}})).

## Commits and pull requests

The history (`git log`) shows the style:

- One short subject line in the imperative, no trailing full stop, saying what changed: `Polish the terminal UI: visible focus, one key notation, no repeated hints`, `Reorganise the tree by feature: no more phase-named files`, `Fix verification follow-ups, add transaction state and web results utilities`.
- A lowercase area prefix, `area: what`, for a change that stays inside one area: `docs: remove webtuidb.1`; older history has `web: add pending.ts`, `tui: add …`, `store: add …`, `menus: add parity_test.go`. Use the folder or feature name.
- Commits made with an AI assistant carry a `Co-Authored-By:` trailer.
- Small, self-contained commits. Nothing in the repository enforces the format.

Pull requests go to `main`, the default branch. Describe what changed and why, name the engines you tested against, and say which front end you drove by hand. For a change to a shortcut or a menu, say how the other front end got the same.

## The `shortcut:` comment convention

When the simplest change that fully solves the task leaves out something a reader might expect, the limit is written down where it lives, in a comment that starts with `shortcut:`:

```go
// shortcut: a line chart is drawn as bars too (the web draws a real line); the rows are the series either way.
// shortcut: only the direct child is killed on Windows; upgrade to a job object for grandchildren.
// shortcut: the job is sampled every 150 ms rather than signalled, upgrade with a cond var if that shows up in a profile.
```

The pattern is: `shortcut:`, what the code does instead, and, when there is one, the upgrade that removes the limit. Rules of thumb:

- Put it on the line or block that has the limit, in the language of the user's problem, not the implementation's.
- A deliberate limit is not a TODO and not a bug: if the behaviour is wrong for users, fix it; if it is a choice, mark it.
- Every `shortcut:` that a user can run into is also stated on the feature's documentation page under "Details and limits".
- There are over a hundred of them: `grep -rn "shortcut:" internal web/src main.go`. The repository enables the `ponytail` Claude Code plugin (`.claude/settings.json`), whose `/ponytail-debt` command lists them as a ledger.

## Review notes

What a reviewer looks at, beyond CI:

1. **Parity.** Did the other front end get the same action, setting, menu entry or dialog? `make parity` catches a missing pair, not a difference in behaviour: drive both.
2. **Data loss and gating.** Does a destructive path confirm, show the SQL, and go through the safe-mode gate and the read-only check? Does the web path re-check on the server?
3. **Engines.** Does the code assume PostgreSQL? Capability flags and the `Dialect` should carry the differences. Is there an integration test, or a reason there is not?
4. **Transactions.** Is a batch atomic where the engine allows (`Capabilities.Transactions`), and does the failure message say when part of it may have run (`partial`)?
5. **The two splitters and classifiers.** A change to `db.Split` or `scan` must keep the fuzz targets green, because a mistake there is a way to hide a write from safe mode.
6. **State and files.** Anything the terminal and the web both write goes through `config.LockFile`. Config and connection files are written with mode 0600.
7. **Documentation.** The page, the screenshot, the `Details and limits` entry.

## Adding a driver

A new engine is a package under `internal/db/<engine>` that implements `db.Session`, registers itself with `db.Register` in `init()`, and is blank-imported in `main.go`; the name goes into `config.Drivers` (and its URL scheme into the URL parser), the capabilities it really has into the session's `Caps()`, a service and a seed into `docker-compose.yml` and `testdata/seed/`, and an integration test next to the driver. The two front ends need no engine-specific code if the capabilities are right; if they do, that is a bug in the capabilities.

## Reporting a bug

Open an issue at [github.com/outofmemory/webtuidb](https://github.com/outofmemory/webtuidb). Run

```sh
webtuidb -bugreport
```

and paste the output. It prints the version, the operating system and architecture, the Go version, `$TERM`, the drivers your connections use, and your `config.toml` with secrets masked (any key that looks like a password, passcode, API key, secret or token, and passwords inside URLs). It never prints `connections.toml`. Then add:

- the front end (terminal or web, and the browser), the engine and its server version (for example "PostgreSQL 16.2"; `-bugreport` lists the drivers, not server versions);
- the steps, starting from a connection and a table;
- what you expected and what happened, with the exact message;
- for a crash, the end of `~/.local/share/webtuidb/crash.log` (the panic, the stack and the last console lines; it is local and nothing is sent anywhere).

`webtuidb -changelog` opens the release notes and `webtuidb -check-update` says whether a newer release exists, which is worth doing first.
