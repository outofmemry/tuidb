package store

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

var historyCSVHeader = []string{"time", "database", "sql", "duration_ms", "source"}

// ExportHistory writes entries as "csv" (time, database, sql, duration_ms, source) or "json" (an array).
func ExportHistory(w io.Writer, entries []HistoryEntry, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", " ")
		return enc.Encode(entries)
	case "csv":
		cw := csv.NewWriter(w)
		cw.Write(historyCSVHeader)
		for _, e := range entries {
			cw.Write([]string{e.Time.Format(time.RFC3339Nano), e.Database, e.SQL, strconv.FormatInt(e.Duration.Milliseconds(), 10), e.Source})
		}
		cw.Flush()
		return cw.Error()
	}
	return fmt.Errorf("unknown history export format %q (csv or json)", format)
}

// ImportHistory reads what ExportHistory wrote; JSON or CSV is told apart by the first byte.
func ImportHistory(r io.Reader) ([]HistoryEntry, error) {
	br := bufio.NewReader(r)
	if b, err := br.Peek(1); err == nil && b[0] == '[' {
		var out []HistoryEntry
		return out, json.NewDecoder(br).Decode(&out)
	}
	rows, err := csv.NewReader(br).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows[0]) < 3 || rows[0][0] != "time" {
		return nil, fmt.Errorf("not a history export: expected a JSON array or a CSV with the header %v", historyCSVHeader)
	}
	var out []HistoryEntry
	for i, row := range rows[1:] {
		if len(row) < len(historyCSVHeader) {
			return nil, fmt.Errorf("line %d: %d fields, want %d", i+2, len(row), len(historyCSVHeader))
		}
		t, err := time.Parse(time.RFC3339Nano, row[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+2, err)
		}
		ms, _ := strconv.ParseInt(row[3], 10, 64)
		out = append(out, HistoryEntry{Time: t, Database: row[1], SQL: row[2], Duration: time.Duration(ms) * time.Millisecond, Source: row[4]})
	}
	return out, nil
}
