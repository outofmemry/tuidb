// setvalue.go: The Set Value dialog and its extras (date, file, expression).

package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

type setValueDate struct{}

type setValueFile struct{}

type setValueExpr struct{}

// maxCellFile caps "Add a file…": the bytes are held in the grid until commit.
const maxCellFile = 16 << 20

func setValueExtras() []pickItem {
	return []pickItem{{Label: "Date picker…", Value: setValueDate{}}, {Label: "Add a file…", Value: setValueFile{}}, {Label: "Advanced…", Value: setValueExpr{}}}
}

// pickSetValue applies the chosen Set Value entry to the cursor cell of g.
func (a *App) pickSetValue(g *Grid, v any) tea.Cmd {
	switch v.(type) {
	case setValueDate:
		f := newForm("Date picker", []formField{
			{Key: "date", Label: "Date (YYYY-MM-DD)", Kind: fText, Value: time.Now().Format(time.DateOnly)},
			{Key: "time", Label: "Time (HH:MM:SS, optional)", Kind: fText},
		}, func(m map[string]string) tea.Cmd {
			lit := strings.TrimSpace(m["date"])
			if tm := strings.TrimSpace(m["time"]); tm != "" {
				lit += " " + tm
			}
			g.SetCell(lit)
			return nil
		})
		f.validate = func(m map[string]string) error {
			if _, err := time.Parse(time.DateOnly, strings.TrimSpace(m["date"])); err != nil {
				return fmt.Errorf("date must look like 2026-10-28")
			}
			if tm := strings.TrimSpace(m["time"]); tm != "" {
				if _, err := time.Parse(time.TimeOnly, tm); err != nil {
					return fmt.Errorf("time must look like 13:04:05")
				}
			}
			return nil
		}
		a.push(f)
	case setValueExpr:
		a.push(newForm("Advanced: SQL expression", []formField{{Key: "expr", Label: "Expression", Kind: fText, Hint: "e.g. now() + interval '1 day'"}}, func(m map[string]string) tea.Cmd {
			if e := strings.TrimSpace(m["expr"]); e != "" {
				if err := checkExpr(e); err != nil {
					return a.fail(err)
				}
				g.SetCell(db.Expr(e))
			}
			return nil
		}))
	case setValueFile:
		a.push(newForm("Add a file", []formField{{Key: "path", Label: "File", Kind: fText}}, func(m map[string]string) tea.Cmd {
			path := strings.TrimSpace(m["path"])
			st, err := os.Stat(path)
			switch {
			case err != nil:
				return a.fail(err)
			case st.Size() > maxCellFile:
				return a.fail(fmt.Errorf("%s is %d MB: files over %d MB are not loaded into a cell", path, st.Size()>>20, maxCellFile>>20))
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return a.fail(err)
			}
			g.SetCell(b)
			return a.say("loaded %d bytes from %s", len(b), path)
		}))
	default:
		g.SetCell(v)
	}
	return nil
}
