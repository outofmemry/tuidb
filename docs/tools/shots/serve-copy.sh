#!/usr/bin/env bash
# A throwaway web server for shots that must not touch (or be touched by) the shared docs server: it runs on its own port
# with private copies of the docs data and config (or WEBTUIDB_DOCS_CONFIG if you point it at a prepared config folder).
#   nohup docs/tools/shots/serve-copy.sh 7798 >/dev/null 2>&1 &      then   node docs/tools/web-shot.mjs --base http://127.0.0.1:7798 ...
# Stop it afterwards (kill the pid; it is a plain webtuidb --frontend process). The copy is in $TMPDIR/webtuidb-shots-<port>.
set -euo pipefail
cd "$(dirname "$0")/../../.."
port=${1:?port}
root="${TMPDIR:-/tmp}/webtuidb-shots-$port"; rm -rf "$root"; mkdir -p "$root"
cp -R docs/tools/env/data "$root/data"
if [ -z "${WEBTUIDB_DOCS_CONFIG:-}" ]; then
  cp -R docs/tools/env/config "$root/config"
  python3 - "$root/config/webtuidb/connections.toml" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
parts = re.split(r'(?m)^(?=\[\[connection\]\])', s)
open(p, 'w').write(''.join(x for x in parts if not re.search(r'(?m)^\s*name = "g"\s*$', x)))
PY
  export WEBTUIDB_DOCS_CONFIG="$root/config"
else
  cp -R "$WEBTUIDB_DOCS_CONFIG" "$root/config"; export WEBTUIDB_DOCS_CONFIG="$root/config"
fi
export WEBTUIDB_DOCS_DATA="$root/data" WEBTUIDB_WEB_PORT=$port
exec docs/tools/serve.sh
