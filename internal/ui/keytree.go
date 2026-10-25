package ui

import (
	"sort"
	"strings"

	"webtuidb/internal/db"
)

// keyNode is one segment of a Redis key name split on ":" (user:1:name -> user > 1 > name).
type keyNode struct {
	seg, path string
	obj       *db.Object // set when a key ends exactly here
	kids      []*keyNode
	idx       map[string]*keyNode
	leaves    int
}

func buildKeyTree(objs []db.Object) *keyNode {
	root := &keyNode{idx: map[string]*keyNode{}}
	for i := range objs {
		o := &objs[i]
		n := root
		for _, seg := range strings.Split(o.Name, ":") {
			c := n.idx[seg]
			if c == nil {
				c = &keyNode{seg: seg, idx: map[string]*keyNode{}}
				c.path = seg
				if n != root { // an empty segment (":x") must still get the parent's prefix
					c.path = n.path + ":" + seg
				}
				n.idx[seg] = c
				n.kids = append(n.kids, c)
			}
			n.leaves++
			n = c
		}
		n.obj = o
		n.leaves++ // counted on the way down for ancestors; the key itself counts once more at its own node
	}
	var fix func(n *keyNode)
	fix = func(n *keyNode) {
		sort.Slice(n.kids, func(i, j int) bool { return n.kids[i].seg < n.kids[j].seg })
		for _, k := range n.kids {
			// user:1:name alone under user:1 would be three levels for one key: merge single-child chains
			for k.obj == nil && len(k.kids) == 1 {
				c := k.kids[0]
				k.seg, k.path, k.obj, k.kids, k.idx = k.seg+":"+c.seg, c.path, c.obj, c.kids, c.idx
			}
			fix(k)
		}
	}
	fix(root)
	return root
}

// flattenKeys lists the visible rows: folders (collapsed unless in open) and keys.
func (s *sidebar) flattenKeys(n *keyNode, depth int, out *[]sbRow) {
	for _, k := range n.kids {
		if len(k.kids) == 0 {
			*out = append(*out, sbRow{kind: sbObject, obj: *k.obj, depth: depth, label: k.seg})
			continue
		}
		folder := sbRow{kind: sbFolder, text: k.seg, path: k.path, depth: depth, open: s.open[k.path], count: countKeys(k)}
		if k.obj != nil {
			folder.obj, folder.hasObj = *k.obj, true
		}
		*out = append(*out, folder)
		if folder.open {
			s.flattenKeys(k, depth+1, out)
		}
	}
}

func countKeys(n *keyNode) int {
	c := 0
	if n.obj != nil {
		c++
	}
	for _, k := range n.kids {
		c += countKeys(k)
	}
	return c
}
