#!/usr/bin/env bash
# Starts the web UI used for the documentation screenshots (foreground) against the docs config.
#   docs/tools/serve.sh            # http://127.0.0.1:7797/#token=devtoken
set -euo pipefail
source "$(dirname "$0")/env.sh"
exec "$WEBTUIDB_BIN" --frontend --listen "127.0.0.1:$WEBTUIDB_WEB_PORT" --token "$WEBTUIDB_WEB_TOKEN" "$@"
