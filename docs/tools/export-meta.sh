#!/usr/bin/env bash
# Exports the resolved keymap and menus from the running docs web server into docs/data/*.json,
# so pages can render bindings with {{< action "commit" >}} and the shortcuts table stays in sync.
set -euo pipefail
source "$(dirname "$0")/env.sh"
curl -sf -H "Authorization: Bearer $WEBTUIDB_WEB_TOKEN" "$WEBTUIDB_WEB_URL/api/meta" | python3 -c '
import sys, json
m = json.load(sys.stdin)
km = m["keymap"]
actions = {}
for b in km["bindings"]:
    actions.setdefault(b["Action"], {"key": b["Key"], "context": b["Context"], "noPrefix": b["NoPrefix"], "desc": b["Desc"], "keys": []})
    actions[b["Action"]]["keys"].append({"key": b["Key"], "context": b["Context"], "noPrefix": b["NoPrefix"]})
root = sys.argv[1]
json.dump({"prefix": km["prefix"], "actions": actions, "bindings": km["bindings"]}, open(root + "/data/keymap.json", "w"), indent=1)
json.dump(m.get("menus", {}), open(root + "/data/menus.json", "w"), indent=1)
json.dump({k: m[k] for k in ("drivers", "safeModes", "sslModes", "operators", "passwordCmdPresets", "export", "tags", "themes", "version") if k in m}, open(root + "/data/meta.json", "w"), indent=1)
print("actions:", len(actions), "bindings:", len(km["bindings"]))
' "$WEBTUIDB_DOCS_ROOT/docs"
