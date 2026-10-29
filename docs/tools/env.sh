# Shared environment for the documentation tooling. Source it, or let the scripts source it.
# Everything runs against an isolated XDG home under docs/tools/env so your own config is never touched.
if [ -n "${ZSH_VERSION:-}" ]; then eval '_src=${(%):-%x}'; else _src="${BASH_SOURCE[0]}"; fi
_here="$(cd "$(dirname "$_src")" && pwd)"
export WEBTUIDB_DOCS_ROOT="$(cd "$_here/../.." && pwd)"
export WEBTUIDB_BIN="${WEBTUIDB_BIN:-$WEBTUIDB_DOCS_ROOT/webtuidb}"
# Each caller may point WEBTUIDB_DOCS_CONFIG at its own config folder (the one that holds webtuidb/config.toml and connections.toml).
export XDG_CONFIG_HOME="${WEBTUIDB_DOCS_CONFIG:-$_here/env/config}"
# Each caller may point WEBTUIDB_DOCS_DATA at its own copy of env/data (the TUI and the web server write state.json there).
export XDG_DATA_HOME="${WEBTUIDB_DOCS_DATA:-$_here/env/data}"
export WEBTUIDB_WEB_PORT="${WEBTUIDB_WEB_PORT:-7797}"
export WEBTUIDB_WEB_TOKEN="${WEBTUIDB_WEB_TOKEN:-devtoken}"
export WEBTUIDB_WEB_URL="http://127.0.0.1:$WEBTUIDB_WEB_PORT"
