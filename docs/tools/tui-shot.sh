#!/usr/bin/env bash
# Drives the terminal UI inside a private tmux server, sends keys, and renders the screen to PNG.
#
#   docs/tools/tui-shot.sh -o docs/static/img/tui/grid.png -c "Shop (Postgres)" -- sleep:2 j j C-d s
#
# Options
#   -o FILE        output PNG (required). The plain-text screen is printed to stdout so you can check it.
#   -c NAME        connection to open at start (passed as the argument to webtuidb); omit for the connection list
#   -a ARGS        extra arguments for webtuidb, one string (e.g. -a "-keys")
#   -s COLSxROWS   terminal size (default 150x42)
#   -b SECONDS     wait after start before the first key (default 2)
#   -d SECONDS     wait between keys (default 0.4)
#   -k             keep the tmux session alive after the shot (socket name printed) for manual poking
#   -t             also write the raw ANSI capture next to the PNG (FILE.ansi)
# Keys use tmux send-keys names: C-d, M-f, S-Tab, Enter, Escape, Tab, Up, Down, BSpace, F5, plain letters.
# Special tokens:  sleep:N  (wait N seconds)   type:TEXT  (type TEXT literally, spaces allowed when quoted)
set -euo pipefail
source "$(dirname "$0")/env.sh"
# Every invocation is appended to docs/tools/shots.log so a screenshot can be regenerated later (docs/tools/reshoot.sh).
printf 'tui\t%s\n' "$(printf '%q ' "$@")" >> "$(dirname "$0")/shots.log"
out=""; conn=""; extra=""; size="150x42"; boot=2; delay=0.4; keep=0; ansi=0
while getopts "o:c:a:s:b:d:kt" opt; do
  case $opt in
    o) out=$OPTARG;; c) conn=$OPTARG;; a) extra=$OPTARG;; s) size=$OPTARG;; b) boot=$OPTARG;; d) delay=$OPTARG;; k) keep=1;; t) ansi=1;;
    *) exit 2;;
  esac
done
shift $((OPTIND-1))
[ -n "$out" ] || { echo "need -o out.png" >&2; exit 2; }
cols=${size%x*}; rows=${size#*x}
sock="wtdocs-$$-$RANDOM"
conf=$(mktemp); cat > "$conf" <<TMUX
set -g default-terminal "xterm-256color"
set -as terminal-features ",*:RGB"
set -g status off
set -s escape-time 0
set -g history-limit 0
set -g mouse off
TMUX
cmd="$WEBTUIDB_BIN $extra"
[ -n "$conn" ] && cmd="$cmd \"$conn\""
TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 \
  command tmux -L "$sock" -f "$conf" new-session -d -s shot -x "$cols" -y "$rows" -- sh -c "$cmd; rc=\$?; echo; echo \"[webtuidb exited with \$rc]\"; sleep 600"
cleanup() { [ $keep = 1 ] || command tmux -L "$sock" kill-server 2>/dev/null || true; rm -f "$conf"; }
trap cleanup EXIT
sleep "$boot"
for k in "$@"; do
  case $k in
    sleep:*) sleep "${k#sleep:}";;
    type:*)  command tmux -L "$sock" send-keys -l -- "${k#type:}"; sleep "$delay";;
    *)       command tmux -L "$sock" send-keys -- "$k"; sleep "$delay";;
  esac
done
sleep 0.3
mkdir -p "$(dirname "$out")"
raw=$(mktemp)
command tmux -L "$sock" capture-pane -p -e -t shot:0 > "$raw"
command tmux -L "$sock" capture-pane -p -t shot:0
[ $ansi = 1 ] && cp "$raw" "$out.ansi"
svg="${out%.png}.svg"
# freeze ignores SGR 49 (default background), which makes every highlight run to the end of the row; normalise first.
fixed=$(mktemp)
python3 "$(dirname "$0")/ansi-fix.py" < "$raw" > "$fixed"
mv "$fixed" "$raw"
freeze --language ansi --font.family "JetBrains Mono, Menlo, monospace" --font.size 13 --line-height 1.2 --window --padding "18,22,18,22" --margin 0 --background "#1a1b26" -o "$svg" "$raw" >/dev/null </dev/null
node "$(dirname "$0")/svg2png.mjs" "$svg" "$out" 2
rm -f "$svg"
rm -f "$raw"
[ $keep = 1 ] && echo "tmux session kept: command tmux -L $sock attach" >&2
echo "wrote $out" >&2
