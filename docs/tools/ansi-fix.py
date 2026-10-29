#!/usr/bin/env python3
"""Normalises a tmux `capture-pane -e` dump for `freeze`.

freeze ignores SGR 49 ("default background"), so a highlighted row whose background is closed with
ESC[49m keeps its background to the end of the line in the rendered SVG, running over the panel behind
a dialog. The terminal bytes are correct; only freeze mishandles them. This filter rewrites every SGR
sequence that contains 49 into a full reset (ESC[0m) followed by the attributes and foreground that are
still active, which freeze renders correctly.

  ansi-fix.py < raw.ansi > fixed.ansi
"""
import re, sys

SGR = re.compile(r'\x1b\[([0-9;:]*)m')

def main():
    data = sys.stdin.buffer.read().decode('utf-8', 'surrogateescape')
    attrs, fg, bg = set(), None, None
    out, pos = [], 0
    for m in SGR.finditer(data):
        out.append(data[pos:m.start()]); pos = m.end()
        raw = m.group(1)
        params = raw.replace(':', ';').split(';') if raw else ['0']
        bg_reset = False
        i = 0
        while i < len(params):
            p = params[i] or '0'
            n = int(p) if p.isdigit() else 0
            if n == 0: attrs, fg, bg = set(), None, None
            elif n in (1, 2, 3, 4, 5, 7, 8, 9): attrs.add(n)
            elif n == 22: attrs -= {1, 2}
            elif n in (23, 24, 25, 27, 28, 29): attrs.discard(n - 20)
            elif n == 39: fg = None
            elif n == 49: bg = None; bg_reset = True
            elif n in (38, 48):
                if i + 1 < len(params) and params[i + 1] == '5': val, step = params[i:i + 3], 3
                elif i + 1 < len(params) and params[i + 1] == '2': val, step = params[i:i + 5], 5
                else: val, step = params[i:], len(params)
                if n == 38: fg = ';'.join(val)
                else: bg = ';'.join(val)
                i += step; continue
            elif 30 <= n <= 37 or 90 <= n <= 97: fg = p
            elif 40 <= n <= 47 or 100 <= n <= 107: bg = p
            i += 1
        if bg_reset:
            seq = ['0'] + [str(a) for a in sorted(attrs)] + ([fg] if fg else []) + ([bg] if bg else [])
            out.append('\x1b[' + ';'.join(seq) + 'm')
        else:
            out.append(m.group(0))
    out.append(data[pos:])
    sys.stdout.buffer.write(''.join(out).encode('utf-8', 'surrogateescape'))

main()
