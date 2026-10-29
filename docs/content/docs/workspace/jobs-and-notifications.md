---
title: Jobs and notifications
description: The jobs centre for imports, exports, backups, restores, searches and diffs, with stop, open folder, download and dismiss, and how you are told a job finished.
weight: 70
---

Long work runs in the background so you can keep working: imports, exports, backups and restores, and Search in
database; the terminal also runs schema and data diffs and history imports as jobs, the web the export panels of
metrics boards. The jobs centre lists them while they run and after they finish, until you
dismiss them.

{{< ui tui="workspace-jobs" web="workspace-jobs" >}}The jobs centre after a Search in database.{{< /ui >}}

## How to open it

| | |
|---|---|
| Terminal | {{< key "C-d M-J" >}} (`jobs`), or **Background jobs** in Open Anything |
| Web | {{< key "C-d M-J" >}}, the **Jobs** button on the right of the status bar, or a `jobs` button in the toolbar (`[web] toolbar`) |

## What it shows

| | Terminal | Web |
|---|---|---|
| Layout | A dialog, **Background jobs · Shop (Postgres)**, one line per job in the order they started. | A drawer on the right, **Jobs**, newest first. |
| Per job | The kind (`import`, `export`, `backup`, `restore`, `search`, `diff`), the title (`export orders-2026-10-29.csv`, `search Springfield`, `schema diff`) and the state: a spinner, `running 12.4s` and the job's last progress message; `✓ done in 3.2s`; `■ stopped`; `✗ <error>`. Under the selected job, the file it wrote. | The kind as a chip, the title and `running`, `done` or `failed`; a progress bar while it runs (filled to the fraction done when the job knows it, a moving bar otherwise); the error text; the last four lines of output of a backup or restore. |
| Empty | `no jobs yet: imports, exports, backups, restores, searches and diffs show up here` | `Imports, exports, backups, restores and searches show up here while they run.` |

The web's **Jobs** button shows a count: the running jobs, highlighted, or else the number of jobs listed.

## Actions

The row menu has **Stop**, **Open folder**, **Download** and **Dismiss**; each UI shows the ones it can do.

| Entry | Terminal | Web | Shows when |
|---|---|---|---|
| Stop | {{< key "s" >}} or {{< key "x" >}} (`stopping export…`) | **Stop** | the job runs (`that job is not running`) |
| Open folder | {{< key "o" >}}: the file in Finder, Explorer, or the folder through `xdg-open` | — | the job wrote a file (`… wrote no file`) |
| Download | listed greyed: `web only: the terminal reveals the file instead` | **Download** saves the finished file through the browser | an export finished without error |
| Dismiss | {{< key "d" >}} or {{< key "Delete" >}} | **Dismiss**; **Dismiss finished** on top dismisses them all | the job is finished (`stop the job before dismissing it`) |

In the terminal {{< key "Enter" >}} or {{< key "m" >}} opens the row menu, ↑ ↓ / {{< key "j" >}} {{< key "k" >}}
move, and {{< key "Esc" >}}, {{< key "q" >}} or {{< key "M-J" >}} close the dialog.

## Running jobs

| | Terminal | Web |
|---|---|---|
| How many at once | One per workspace: starting a second says `another export, import or search is still running (cancel it first)`. | Up to four per session: `4 background jobs are already running on this session; wait or stop one`. |
| Stop the running work | {{< key "C-d ." >}} stops the running job (the same key opens the process list when nothing runs), or Stop in the jobs centre. | Stop in the jobs centre, or the dialog that started it. |
| Progress | The status line shows the job's messages as it goes (`exported 3 of 5 tables…`, `importing history… 500 of 1200 read, 480 new`), then `export orders.csv: stopped`, the error, or the result. | Polled every second while the drawer is open or a job runs, every four seconds otherwise. |
| Logged | Each job is a line in the [console](../console/) when it ends, with its duration. | Imports, backups and restores are logged in the console. |

A job of the terminal belongs to its workspace and is listed in that workspace's jobs centre. A web job belongs to the session on the server and ends when the session closes.

### The web's disk budget

An export in the web is written to a temporary file on the server, kept until you download or dismiss it, and an
import is spooled to disk while it runs. Each session gets 1 GiB for these files. When a new import would not
fit, the oldest finished exports that were not downloaded are deleted to make room; if it still does not fit,
the server answers `this session's temporary files are over their 1024 MB budget; download or delete finished
exports and retry`. At most three finished, not downloaded exports are kept per session (starting a fourth job
removes the oldest), and at most 30 finished jobs without a file stay in the list.

## When a job finishes

| | Terminal | Web |
|---|---|---|
| On screen | The status line says how it ended: the result, `<job>: stopped`, or the error in red. | The jobs centre and the dialog that started the job. |
| While you are elsewhere | If the terminal window does not have the focus, a finished, failed or stopped job rings the bell and sends a desktop notification (`Shop (Postgres) export orders.csv: finished`) through OSC 9 (iTerm2, Windows Terminal, WezTerm…) and OSC 777 (VTE terminals, Konsole…). Terminals that know neither ignore them. | If the page is hidden, a desktop notification (`search Springfield finished`, or `… failed` with the error). It needs `[web] notifications = true`; the browser asks for permission the first time you start an export, import or backup. |

The terminal knows whether it has the focus from the terminal's focus reports; a terminal that does not send them
never gets the notification.

## Reveal in file manager

**Open folder** (jobs centre) and **Reveal in file manager** (on a saved query) show the file in the system file
manager of the machine webtuidb runs on: `open -R` on macOS, `explorer /select,` on Windows, `xdg-open` on the
folder elsewhere. When that fails, the path is shown instead. In the web, Reveal works only when the browser runs
on the same machine as the server (a loopback connection); from another machine the server refuses
(`reveal works only on the machine running webtuidb`) and the web copies the path for you instead.

## Settings

| Setting | Effect |
|---|---|
| `[web] notifications` | Desktop notifications in the web when a job finishes while the page is hidden (default off). |
| `[web] toolbar` | Add `jobs` to put a Jobs button in the web toolbar. |
