---
title: Passcode lock
description: Lock webtuidb with a passcode at start, after idle minutes or on demand, in the terminal and in the browser; set it with -set-passcode.
weight: 30
---

A passcode keeps someone at your unlocked computer out of webtuidb. With a passcode set, the app starts
locked, locks itself after a number of idle minutes if you want, and locks on {{< key "C-d M-L" >}}. Nothing of
the workspace is visible while it is locked, and your open tabs and unsent edits are still there after you
unlock.

{{< ui tui="safety-lock-screen" web="safety-lock-screen" >}}The lock screen after one wrong passcode.{{< /ui >}}

## Setting a passcode

The passcode is stored as a bcrypt hash in `config.toml`, never in plain text. Make the hash with:

```sh
webtuidb -set-passcode
```

{{< ui tui="safety-set-passcode" >}}`-set-passcode` asks twice and prints the line for `config.toml`.{{< /ui >}}

It asks "New passcode:" and "Repeat it:" without echoing, then prints where to put it ("Put this in
…/config.toml:") on standard error and the lines themselves on standard output:

```toml
[security]
passcode = "$2a$10$…"
lock_after_mins = 10   # optional, add it yourself
```

Paste them into `config.toml` (or, when the file has no `[security]` table yet, run
`webtuidb -set-passcode >> ~/.config/webtuidb/config.toml`) and restart webtuidb.

| Message | Why |
|---|---|
| `set-passcode needs a terminal to read the passcode from` | standard input is not a terminal (a pipe or a script) |
| `the passcodes differ; nothing was changed` | the two entries were not the same |
| `the passcode must not be empty` | you pressed Enter twice |
| ``security.passcode must be a bcrypt hash; make one with `webtuidb -set-passcode` `` | `config.toml` holds something that does not start with `$2` (a plain-text passcode, for example); webtuidb refuses to start |

`-set-passcode` cannot be combined with other modes such as `--frontend`. To change the passcode, run it again and
replace the line; to remove the lock, delete the `passcode` line. The settings screen does not offer
`security.passcode` (nor `web.password`), so it cannot be changed or revealed from inside the app.

Because a short passcode's bcrypt hash can be guessed offline, webtuidb sets `config.toml` to mode `0600` when a
passcode is in it (see [Secrets and files](../secrets-and-files/)).

## When it locks

| Trigger | Terminal | Web |
|---|---|---|
| Start | the app opens on the lock screen | the page opens on the lock screen; nothing loads behind it until the settings are known |
| `lock` action | {{< key "C-d M-L" >}} | {{< key "C-d M-L" >}} in a workspace |
| Idle | no key press for `lock_after_mins` minutes (checked every 15 seconds) | no mouse move, click, key, wheel or touch for `lock_after_mins` minutes |
| Reload | — | reloading the browser tab locks again |

Without a passcode the `lock` action answers "no passcode is set: run webtuidb -set-passcode and add the line it
prints to config.toml" in the terminal and "No passcode is set: run webtuidb -set-passcode and add the [security]
line" in the browser.

## The lock screen

**Terminal:** a box "🔒 Locked" with "webtuidb is locked." and a hidden **passcode** field. The rest of the
screen is blank, and no dialog can open over it.

| Key | What it does |
|---|---|
| typing, pasting | fills the field (shown masked) |
| {{< key "Enter" >}} | unlock; a wrong passcode clears the field and shows "wrong passcode (1 of 3 tries)" |
| {{< key "Esc" >}}, {{< key "C-c" >}} | nothing |
| {{< key "C-d q" >}} | quit (the bound `quit` key after the prefix; no other action works while locked) |

After the third wrong passcode the screen says "wrong passcode: wait 10 s" and ignores every key for 10 seconds
("too many wrong tries: wait N s" if you keep typing); then you get three more tries.

Quitting from the lock screen cannot open a dialog, so when a workspace has uncommitted changes the first
{{< key "C-d q" >}} only warns: "uncommitted changes will be lost: press C-d q again to quit". The second one
quits.

**Web:** a page "Locked" with a **Passcode** field and an **Unlock** button (disabled while the field is empty).
The workspace behind it is hidden and inert, so it is neither visible nor reachable with Tab, and no shortcut
reaches it while the lock is up. A wrong passcode shows "Wrong passcode". There is no quit; close the browser
tab instead.

The browser asks the server to check the passcode (`POST /api/passcode/check`); the hash never leaves the server.
After three wrong answers from one address, every further attempt from that address waits one second before it is
answered; a right answer resets the count, and an address that stays quiet for 15 minutes starts again.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `[security] passcode` | unset | bcrypt hash from `webtuidb -set-passcode`; unset means no lock |
| `[security] lock_after_mins` | `0` | idle minutes before the lock screen; `0` never locks on idle. A negative value stops `config.toml` with `security.lock_after_mins must be >= 0 (0 = never)` |

Rebind the `lock` action under `[keys]`; see the [configuration reference](../../configuration/).

## Details and limits

- The lock protects the screen, not the data on disk: `connections.toml`, history and saved queries are files
  anyone with your account can read.
- In the browser the access token stays in the tab's session storage while it is locked (marked as a known
  shortcut in the code). The lock keeps a bystander out of the page, not out of the API: someone who can run
  scripts in that browser profile could still call the API. A server-side session lock is the planned upgrade.
- Each browser tab locks on its own. Locking one tab does not lock the terminal UI or other tabs, and the idle
  timer of each tab counts only activity in that tab.
- The web server's login page (`[web] password`) is a separate check with its own lockout; see
  [Web security](../web-security/#password-login).
