---
title: Install
description: Get the webtuidb binary from a release, Homebrew, Docker or a source build, then add completions and the man page.
weight: 10
---

webtuidb is a single Go binary. The terminal UI needs nothing else; the browser app is embedded into the same binary when it is built with the web assets.

## Release binaries

Download the archive for your platform (Linux, macOS, Windows; amd64 and arm64) from the project's Releases page, unpack it and put `webtuidb` on your `PATH`. The archive also contains the man page `man/webtuidb.1`.

## Homebrew

```bash
brew install --HEAD outofmemry/tap/webtuidb
```

## From source

You need Go (the version in `go.mod`) and, for the web UI, Node 20 or newer.

```bash
make web build
```

`make web` builds the browser app into `internal/web/dist`, and `build` runs `go build -o webtuidb .`, which embeds it. A plain `go build` or `go install .` from a clone works too, but without `make web` first the binary serves the API only and has no browser app. `go install webtuidb@latest` does not work: the module is named `webtuidb`, which the Go tool refuses as a remote path.

## Docker

The image serves the web UI on port 7788 and reads your normal configuration folder when you mount it:

```bash
docker build -t webtuidb .
docker run -p 127.0.0.1:7788:7788 -e WEBTUIDB_PASSWORD='choose-one' --user $(id -u) \
  -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb webtuidb
```

Inside the container webtuidb listens on `0.0.0.0` over plain HTTP, so it refuses to start unless `WEBTUIDB_PASSWORD` or `WEBTUIDB_TOKEN` is set (`WEBTUIDB_INSECURE=1` accepts an open server explicitly). `--user` lets it read and chmod your bind-mounted files. Publish on `127.0.0.1` and put TLS in front of anything beyond that. See [Serving the web UI](../../web-ui/serving/) for every flag and variable.

## Completions and man page

```bash
source <(webtuidb -completion bash)        # or zsh; fish: webtuidb -completion fish | source
webtuidb -man | sudo tee /usr/local/share/man/man1/webtuidb.1
```

Completions cover the flags and your saved connection names (`-list-connections` is what the scripts call).

## Checking the version

`webtuidb -version` prints the version. `webtuidb -check-update` asks GitHub once whether a newer release exists; nothing checks on its own. `webtuidb -changelog` opens the release notes in your browser and prints the URL. `webtuidb -bugreport` prints version, OS, `$TERM`, the drivers your connections use and your config with secrets masked, ready to paste into an issue. The full flag list is in the [CLI reference](../../automation/cli/).

## Where it keeps things

| Path | Holds |
|---|---|
| `~/.config/webtuidb/config.toml` | settings and key bindings |
| `~/.config/webtuidb/connections.toml` | saved connections (mode 0600; passwords are plain text unless you use `password_cmd`) |
| `~/.local/share/webtuidb/` | query history, saved queries, UI state, boards, chats, crash log |

`$XDG_CONFIG_HOME` and `$XDG_DATA_HOME` are honoured. The first run moves an old `tuidb` folder to the new name, so connections, history and state carry over from the earlier release.
