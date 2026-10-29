# Documentation tooling

Everything here regenerates the screenshots in `docs/static/img` against the compose test databases
(`make up` in the repo root) plus the demo "shop" dataset.

```sh
make up                                   # test databases
docs/tools/seed.sh                        # demo data (re-runnable)
make web build                            # the binary the screenshots use (./webtuidb)
docs/tools/serve.sh &                     # web UI on http://127.0.0.1:7797/#token=devtoken (docs config, not yours)
docs/tools/tui-shot.sh -o docs/static/img/tui/example.png -c "Shop (Postgres)" -- sleep:2 C-d e
node docs/tools/web-shot.mjs --out docs/static/img/web/example.png --steps '[{"click":"text=Shop (Postgres)"},{"wait":1500}]'
```

The TUI script needs `tmux` and `freeze` (`brew install charmbracelet/tap/freeze`); the web script needs
`npm install` in this folder once (`npx playwright install chromium`). Both run with `XDG_CONFIG_HOME` and
`XDG_DATA_HOME` pointing at `docs/tools/env`, so your own `~/.config/webtuidb` is never read or written.
