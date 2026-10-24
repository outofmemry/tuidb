package db

import (
	"bytes"
	"encoding/csv"
	"fmt"
)

// Preimage renders the original values of every UPDATE / DELETE change as CSV (one header
// and one row per change) for history when [general] log_original_values is on.
// shortcut: a change only carries its key values (every original value on tables without a
// primary key), so the pre-image is those, not the whole row; fetch the row before Plan to upgrade.
func Preimage(changes []Change) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, c := range changes {
		if c.Kind == Insert || len(c.Key) == 0 {
			continue
		}
		cols, vals := make([]string, len(c.Key)), make([]string, len(c.Key))
		for i, kv := range c.Key {
			cols[i], vals[i] = kv.Col, FormatValue(kv.Val)
		}
		w.Write(append([]string{fmt.Sprintf("%s %s", map[ChangeKind]string{Update: "update", Delete: "delete"}[c.Kind], c.Name)}, cols...))
		w.Write(append([]string{""}, vals...))
	}
	w.Flush()
	return buf.String()
}
