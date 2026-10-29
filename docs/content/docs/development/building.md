---
title: Building from source
description: Prerequisites, make targets, how the web app is embedded in the binary, version stamping, release archives, Docker and cross-platform notes.
weight: 10
---

webtuidb is one Go binary. The terminal UI, the HTTP server and the web app all live in it: the web app is built with Node, copied into `internal/web/dist`, and embedded into the binary with `go:embed`. This page covers every way to build that binary.

## Prerequisites

| Tool | Version | Needed for |
|---|---|---|
| Go | the version on the `go` line of `go.mod` (1.27.3 at the time of writing); CI reads it with `go-version-file: go.mod` | every build |
| Node.js and npm | 20.19 or newer on the 20 line, or 22.12 or newer (Vite 8's engine range); CI and the Dockerfile use Node 22 | the web app (`make web`) |
| Docker with Compose | any recent version | the test databases (`make up`), `make docker` |
| goreleaser | 2.x (the config says `version: 2`) | `make release` only |
| Hugo (extended) | 0.169.0, the version the docs workflow pins | the documentation site only |

The binary has no cgo dependencies: every build, including the release archives and the Docker image, uses `CGO_ENABLED=0`. A cross-compile needs nothing but Go.

## Build targets

All targets are in the `Makefile` at the repository root.

| Command | What it does |
|---|---|
| `make web` | `cd web && npm ci && npm run build`, then `touch internal/web/dist/.gitkeep`. `npm run build` is `tsc --noEmit && vite build && touch ../internal/web/dist/.gitkeep`, so a type error fails the build. Vite writes into `internal/web/dist` (`emptyOutDir: true`). |
| `make build` | `go build -o webtuidb .`. Does not touch Node. |
| `make web build` | The usual full build: the web app first, then the binary with the app embedded. |
| `go install .` | Installs into `$GOBIN`. Run it from a checkout, after `make web` if you want the web UI. |
| `make release` | `goreleaser release --snapshot --clean`: the cross-platform archives in `dist/`, nothing published. |
| `make docker` | `docker build -t webtuidb --build-arg VERSION=$(git describe --tags --always --dirty) .` |

{{< callout type="warning" >}}
`go install webtuidb@latest` does not work: the module is named `webtuidb` (no dotted host in the path), so the Go tool refuses it with `malformed module path "webtuidb": missing dot in first path element`. Build from a clone with `go install .` or `make build`.
{{< /callout >}}

### API and terminal UI only

`make build` on its own is a complete terminal UI. The web server still starts with `--frontend`, but if `internal/web/dist/index.html` is missing from the binary it prints this on stderr and keeps serving the API:

```
webtuidb: the web app is not built into this binary; run `make web` then `make build` (the API still works)
```

A binary built with `go install` from a clone that has never run `make web` is in this state.

## How the web app is embedded

`internal/web/server.go` has `//go:embed all:dist`. Everything under `internal/web/dist` goes into the binary, and the server serves it from memory: unknown paths fall back to `index.html` so the single-page router works.

- `internal/web/dist/*` is git-ignored (`.gitignore`), except `.gitkeep`.
- `.gitkeep` exists so that `//go:embed all:dist` always has at least one file to embed. A fresh clone therefore compiles before you have ever run Node; it just has no web app. Both `make web` and `npm run build` re-create the file because `vite build` empties the folder.
- `.dockerignore` excludes `internal/web/dist`, `web/node_modules`, `dist`, `.git` and the `webtuidb` binary, so the Docker image always builds its own copy of the web app.

The man page is embedded the same way: `main.go` has `//go:embed man/webtuidb.1`, and `webtuidb -man` prints it. The file must exist at build time, and the release archives include it (`files: [README.md, man/webtuidb.1]` in `.goreleaser.yaml`).

## Version stamping

`main.go` declares `var Version = "dev"`. `webtuidb -version` prints `webtuidb <version>`; the same string is shown in `GET /api/meta` (`version`), by `-bugreport` and by `-check-update`. Three things can set it:

1. **`-ldflags "-X main.Version=…"`** is how release builds do it. The Dockerfile passes `-trimpath -ldflags "-s -w -X main.Version=${VERSION}"`; goreleaser passes `-s -w -X main.Version={{.Version}}`; `make docker` feeds the Dockerfile `git describe --tags --always --dirty`.
2. **The module version the Go toolchain recorded.** When `Version` is still `dev`, `versionString()` falls back to the build information, unless that is empty or `(devel)`. A plain `go build` or `go install .` inside a git checkout is stamped by the toolchain with a pseudo-version such as `v0.0.0-20261029061057-945f583041c5+dirty` (`+dirty` when the tree has uncommitted changes).
3. Otherwise it stays `dev`.

`git describe --tags --always --dirty` gives the short commit hash when the repository has no tags, which is the case today, so a `make docker` image reports a hash such as `945f583`.

`-check-update` (never automatic) asks `api.github.com/repos/outofmemory/webtuidb/releases/latest` once, with a five second timeout, and says whether the tag is newer. It compares dotted numbers (`v1.2.3`, `1.2`); a version such as `dev` or a hash is not comparable, so it never reports an update for those.

## Release archives

`.goreleaser.yaml` (`make release`):

- `before.hooks`: `make web`, so every archive embeds the web app.
- Builds `linux`, `darwin` and `windows`, each for `amd64` and `arm64` (six binaries), `CGO_ENABLED=0`.
- Archives are `tar.gz`, `zip` on Windows, named `webtuidb_<version>_<os>_<arch>`, and contain the binary, `README.md` and `man/webtuidb.1`.
- `checksums.txt`, and a changelog sorted ascending.

`--snapshot` skips the publish step and the tag requirement; `--clean` empties `dist/` first. The archives land in `dist/` (git-ignored).

## Docker

The `Dockerfile` has three stages:

1. `node:22-alpine`: `npm ci` and `npm run build` in `web/` (Vite writes to `../internal/web/dist`).
2. `golang:1.27-alpine`: `go mod download`, copy the source and the built app, then `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}"`. `VERSION` is a build argument, default `dev`.
3. `gcr.io/distroless/static-debian12:nonroot`: only the binary. `EXPOSE 7788`, and the entry point is `/webtuidb --frontend --listen 0.0.0.0:7788 --insecure-listen`.

Because the container listens on all interfaces over plain HTTP, the server refuses to start unless `WEBTUIDB_PASSWORD` or `WEBTUIDB_TOKEN` is set (or you accept an open server with `WEBTUIDB_INSECURE=1`). Serving the web UI, including the container, is covered on the [Web UI]({{< relref "/docs/web-ui" >}}) page.

## Developing the web app

Run the Go server and the Vite dev server side by side:

```sh
./webtuidb --frontend --token dev        # the API on 127.0.0.1:7788
cd web && npm run dev                    # Vite on http://localhost:5173
```

Open `http://localhost:5173/#token=dev`. Vite proxies `/api` to `http://127.0.0.1:7788`; set `WEBTUIDB_API` to point it at another address (the repository's `.claude/launch.json` uses `http://127.0.0.1:7799`).

The proxy must keep `changeOrigin: false` (a comment in `web/vite.config.ts` says so). The server checks the `Origin` header of every API call against the `Host` it was sent to, to stop DNS-rebinding and cross-site requests, and a rewritten `Host` would break that check. On a loopback listener the server accepts an origin that equals the request's `Host`, which is what the proxy produces. If you serve the front end from another address, list that origin in `WEBTUIDB_DEV_ORIGIN` (comma separated, for example `http://localhost:5173`); the server prints a warning at start-up whenever the variable is set, and the token is still required.

`web/DESIGN.md` holds the design rules for the web UI (palette tokens, one popover style, the quality floor); read it before changing styles.

## Cross-platform notes

- **Config lock.** Files that the terminal UI and a `--frontend` process may both rewrite (`connections.toml`, `state.json`, the history files and saved queries) are protected by `config.LockFile`, an exclusive lock on `<file>.lock`. On Unix it is `flock(LOCK_EX)` (`internal/config/lock_unix.go`); on Windows it is `LockFileEx` (`internal/config/lock_windows.go`). Both block until the holder releases the lock.
- **Clipboard.** `internal/clip` writes an OSC 52 escape to the controlling terminal (`/dev/tty`) when it detects an SSH session (`SSH_TTY` or `SSH_CONNECTION` set) or when `WEBTUIDB_OSC52=1`; that copies to the machine your terminal is on. Otherwise it runs the first tool it finds: `pbcopy` on macOS, `clip` on Windows, `wl-copy` (when `WAYLAND_DISPLAY` is set), `xclip` or `xsel` (when `DISPLAY` is set) on Linux. Some terminals drop OSC 52 payloads of around 100 KB or more, and Windows `clip.exe` reads the ANSI code page, so non-ASCII text can arrive garbled there; both are marked `shortcut:` in the code.
- **Backup and restore.** `internal/backup` starts `pg_dump`, `mysqldump` and friends in their own process group on Unix, so cancelling kills the whole group. On Windows only the direct child is killed (`shortcut:` in `proc_windows.go`).
- **Opening files and URLs.** `open`, `rundll32 url.dll,FileProtocolHandler` or `xdg-open` by platform, for `--open`, `-changelog` and "Open Anything".
- **Paths.** Config is `$XDG_CONFIG_HOME/webtuidb` and data `$XDG_DATA_HOME/webtuidb`, falling back to `~/.config/webtuidb` and `~/.local/share/webtuidb` on every OS, Windows included. Relative `XDG_*` values are ignored. If the home directory cannot be found, the program stops with a message instead of writing into the working directory.

The `shortcut:` markers are collected on the [Contributing]({{< relref "contributing" >}}) page.
