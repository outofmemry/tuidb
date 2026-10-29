#!/usr/bin/env bash
# Clears the remembered filters, filter bar and sort of one table in the docs web server, so the next shot starts clean.
#   docs/tools/shots/reset.sh "Shop (Postgres)" shop public.orders
set -euo pipefail
url="http://127.0.0.1:${WEBTUIDB_WEB_PORT:-7797}/api/state/table"
q=$(python3 -c "import sys,urllib.parse as u;print(u.urlencode({'conn':sys.argv[1],'db':sys.argv[2],'table':sys.argv[3]}))" "$1" "$2" "$3")
curl -fsS -X PUT -H "Authorization: Bearer ${WEBTUIDB_WEB_TOKEN:-devtoken}" -H 'Content-Type: application/json' \
  -d '{"filters":{"Filters":[],"Or":false},"show_filters":false,"sort":[]}' "$url?$q" >/dev/null
