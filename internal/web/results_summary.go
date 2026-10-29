package web

import (
	"fmt"
	"net/http"
	"strings"
	"webtuidb/internal/db"
)

const summarizeOver = 100

// runSummary runs every statement and returns one item: how many ran, how many failed and the rows they touched.
func (ss *session) runSummary(r *http.Request, sess db.Session, stmts []db.Stmt, limit int) execItem {
	var failed, ran int
	var rows, got, ms int64
	var firstErrs []string
	for _, st := range stmts {
		it := ss.execOne(r, sess, st, limit)
		ran++
		ms += it.DurationMs
		rows += it.Affected
		if it.HasRows {
			got += int64(len(it.Cells))
		}
		if it.Error != "" {
			failed++
			if len(firstErrs) < 5 {
				firstErrs = append(firstErrs, fmt.Sprintf("statement %d: %s", ran, it.Error))
			}
			if ss.srv.o.Cfg.Editor.ReturnOnError {
				break
			}
		}
		if r.Context().Err() != nil {
			break
		}
	}
	msg := fmt.Sprintf("%d statements run, %d failed, %d rows affected, %d rows returned", ran, failed, rows, got)
	if len(stmts) > ran {
		msg += fmt.Sprintf(" (%d not run)", len(stmts)-ran)
	}
	if len(firstErrs) > 0 {
		msg += "\n" + strings.Join(firstErrs, "\n")
	}
	it := execItem{Stmt: fmt.Sprintf("%d statements", len(stmts)), Label: "Summary"}
	it.Message, it.Affected, it.DurationMs = msg, rows, ms
	return it
}
