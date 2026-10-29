#!/usr/bin/env bash
# docs/tools/shots/tui.sh <stem> [tui-shot args after -o]: a terminal shot against a fresh private copy of docs/tools/env/data,
# so remembered filters and sorts of earlier shots (or of the web server) never leak into it. Retries when Postgres is busy.
#   docs/tools/shots/tui.sh browsing-filter-bar -c "Shop (Postgres)" -- sleep:2 C-d p ...
cd "$(dirname "$0")/../../.."
stem=$1; shift
d=$(mktemp -d); cp -R docs/tools/env/data/. "$d/"
export WEBTUIDB_DOCS_DATA=$d
# Connection screens also use a private copy of the config, so a key path that saves something cannot change the docs connections.
# (A replayed key path once saved a stray connection named "g" into docs/tools/env/config; it is left out here.)
if [ -z "${WEBTUIDB_DOCS_CONFIG:-}" ]; then
  c=$(mktemp -d); cp -R docs/tools/env/config/. "$c/"
  python3 - "$c/webtuidb/connections.toml" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
parts = re.split(r'(?m)^(?=\[\[connection\]\])', s)
open(p, 'w').write(''.join(x for x in parts if not re.search(r'(?m)^\s*name = "g"\s*$', x)))
PY
  export WEBTUIDB_DOCS_CONFIG=$c
fi
trap 'rm -rf "$d"' EXIT
for i in 1 2 3 4 5; do
  txt=$(timeout 120 docs/tools/tui-shot.sh -o "docs/static/img/tui/$stem.png" "$@" 2>&1)
  if echo "$txt" | grep -qE "too many clients"; then echo "retry $i (postgres busy)" >&2; sleep 30; continue; fi
  echo "$txt"; exit 0
done
echo "$txt"; exit 1
