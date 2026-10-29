// editor_blocks.go: Rectangular (block) selection in the editor.

package ui

import (
	"strings"
)

// blockRect is the rectangle of block mode: rows r0..r1, columns c0..c1-1 (ok is false while it is empty).
func (e *Editor) blockRect() (r0, r1, c0, c1 int, ok bool) {
	if e.blk == nil {
		return
	}
	r0, r1 = min(e.blk.row, e.cur.row), max(e.blk.row, e.cur.row)
	c0, c1 = min(e.blk.col, e.cur.col), max(e.blk.col, e.cur.col)
	return r0, r1, c0, c1, c1 > c0
}

func (e *Editor) blockText() string {
	r0, r1, c0, c1, _ := e.blockRect()
	var out []string
	for r := r0; r <= r1; r++ {
		rs := e.runes(r)
		if c0 >= len(rs) {
			out = append(out, "")
			continue
		}
		out = append(out, string(rs[c0:min(c1, len(rs))]))
	}
	return strings.Join(out, "\n")
}

// deleteBlock cuts the rectangle out of every row it covers and leaves block mode.
func (e *Editor) deleteBlock() bool {
	r0, r1, c0, c1, ok := e.blockRect()
	if !ok {
		return false
	}
	for r := r0; r <= r1; r++ {
		if rs := e.runes(r); c0 < len(rs) {
			e.lines[r] = string(rs[:c0]) + string(rs[min(c1, len(rs)):])
		}
	}
	e.cur, e.anchor, e.blk = pos{r0, c0}, nil, nil
	e.cache.dirty = true
	return true
}

// toggleBlock is block_select: the cursor becomes one corner of a rectangle that the arrow keys stretch.
func (e *Editor) toggleBlock() string {
	if e.blk != nil {
		e.blk = nil
		return "block selection off"
	}
	p := e.cur
	e.blk, e.anchor = &p, nil
	return "block selection: arrows stretch it, ctrl+c copies, delete cuts, V or Esc ends it"
}

func (e *Editor) blockBadge() string {
	if e.blk != nil {
		return " [BLOCK]"
	}
	return ""
}
