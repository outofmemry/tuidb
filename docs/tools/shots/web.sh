#!/usr/bin/env bash
# docs/tools/shots/web.sh <stem> [table]: reset the remembered state of the table, run docs/tools/shots/<stem>.mjs,
# save web/<stem>.png, reset again.
set -uo pipefail
cd "$(dirname "$0")/../../.."
stem=$1; shift
tbl=${1:-public.orders}
docs/tools/shots/reset.sh "Shop (Postgres)" shop "$tbl"
node docs/tools/web-shot.mjs --out "docs/static/img/web/$stem.png" --script "docs/tools/shots/$stem.mjs" --text 2>&1
docs/tools/shots/reset.sh "Shop (Postgres)" shop "$tbl"
