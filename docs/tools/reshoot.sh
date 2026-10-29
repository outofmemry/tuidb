#!/usr/bin/env bash
# Replays docs/tools/shots.log (the last recorded invocation per output file) to regenerate screenshots.
#   docs/tools/reshoot.sh                 # every screenshot
#   docs/tools/reshoot.sh browsing-       # only outputs whose name contains the text
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"; root="$(cd "$here/../.." && pwd)"; cd "$root"
filter="${1:-}"
# keep the last line per output path
python3 - "$here/shots.log" "$filter" <<'PY' > "$here/.reshoot-plan"
import sys, re, shlex
log, filt = sys.argv[1], sys.argv[2]
last = {}
for line in open(log):
    kind, _, args = line.rstrip("\n").partition("\t")
    toks = shlex.split(args)
    out = None
    if kind == "tui":
        for i, t in enumerate(toks):
            if t == "-o" and i + 1 < len(toks): out = toks[i + 1]
    else:
        for i, t in enumerate(toks):
            if t == "--out" and i + 1 < len(toks): out = toks[i + 1]
    if out and (not filt or filt in out): last[out] = (kind, args)
for out, (kind, args) in last.items(): print(f"{kind}\t{args}")
PY
n=0; fail=0
while IFS=$'\t' read -r kind args; do
  n=$((n+1))
  if [ "$kind" = tui ]; then eval "docs/tools/tui-shot.sh $args" >/dev/null 2>&1 || { fail=$((fail+1)); echo "FAILED tui $args" >&2; }
  else eval "node docs/tools/web-shot.mjs $args" >/dev/null 2>&1 || { fail=$((fail+1)); echo "FAILED web $args" >&2; }
  fi
done < "$here/.reshoot-plan"
rm -f "$here/.reshoot-plan"
echo "reshot $n screenshots, $fail failed"
