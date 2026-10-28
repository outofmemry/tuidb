package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/tools"
)

// Data tools (M5): schema diff, data diff, bulk update, fake data. The statements come from
// internal/tools and go through the usual safe-mode gate (execute); schema diff only writes SQL into an editor.

// maxFakeRows caps the fake data generator, like the web UI.
const maxFakeRows = 5000

// toolValue reads a typed value: NULL is SQL NULL, "=expr" is a free expression, anything else a literal.
func toolValue(s string) any {
	switch {
	case s == "NULL":
		return db.Null
	case strings.HasPrefix(s, "="):
		return db.Expr(strings.TrimSpace(s[1:]))
	}
	return s
}

// checkExpr refuses an =expr value that could carry a second statement.
// shortcut: local copy until db.CheckExpr exists; it is a token check, not a parser.
func checkExpr(e string) error { return db.CheckExpr(e) }

// tableDefs reads every table of a schema as the structure the planners work on.
func tableDefs(ctx context.Context, sess db.Session, schema string, progress func(done, total int)) ([]db.TableDef, error) {
	objs, err := sess.Objects(ctx, schema)
	if err != nil {
		return nil, err
	}
	var out []db.TableDef
	total := 0
	for _, o := range objs {
		if o.Kind == db.KindTable {
			total++
		}
	}
	for _, o := range objs {
		if o.Kind != db.KindTable {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if progress != nil {
			progress(len(out), total)
		}
		cols, err := sess.Columns(ctx, o.ObjRef)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", o.Name, err)
		}
		d := db.TableDef{Schema: schema, Name: o.Name, Columns: cols}
		for _, c := range cols {
			if c.PK {
				d.PrimaryKey = append(d.PrimaryKey, c.Name)
			}
		}
		if sess.Caps().Indexes {
			if d.Indexes, err = sess.Indexes(ctx, o.ObjRef); err != nil {
				return nil, fmt.Errorf("%s indexes: %w", o.Name, err)
			}
		}
		if sess.Caps().ForeignKeys {
			if d.ForeignKeys, err = sess.ForeignKeys(ctx, o.ObjRef); err != nil {
				return nil, fmt.Errorf("%s foreign keys: %w", o.Name, err)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// sqlFamily groups the drivers that speak one SQL dialect, so a diff never crosses engines.
func sqlFamily(driver string) string {
	switch driver {
	case config.MySQL, config.MariaDB:
		return "mysql"
	case config.Postgres, config.Cockroach:
		return "postgres"
	}
	return driver
}

// schemaDiff compares a schema of this connection with a schema of this or another connection and writes
// the statements that turn the first into the second into a new editor tab (nothing is sent).
func (w *Workspace) schemaDiff() tea.Cmd {
	if w.caps.Editor != "sql" {
		return w.a.say("schema diff is for SQL engines")
	}
	others := []string{"this connection"}
	for _, c := range w.a.d.Conns {
		if c.Name != w.conn.Name && sqlFamily(c.Driver) == sqlFamily(w.conn.Driver) { // same dialect only
			others = append(others, c.Name)
		}
	}
	hasSchemas := func(map[string]string) bool { return w.caps.Schemas }
	w.a.push(newForm("Schema diff: turn FROM into TO", []formField{
		{Key: "from", Label: "FROM schema (the SQL runs here)", Kind: fText, Value: w.sess.CurrentSchema(), Show: hasSchemas},
		{Key: "with", Label: "TO is in", Kind: fChoice, Options: others, Value: others[0]},
		{Key: "to", Label: "TO schema", Kind: fText, Hint: "empty: that connection's current schema", Show: hasSchemas},
		{Key: "drops", Label: "Include DROP TABLE for tables missing in TO", Kind: fBool, Hint: "off: they are written as comments"},
	}, func(v map[string]string) tea.Cmd {
		from, to, with := strings.TrimSpace(v["from"]), strings.TrimSpace(v["to"]), v["with"]
		withDrops := v["drops"] == "true"
		if with == others[0] && (from == to || !w.caps.Schemas) {
			return w.a.fail(fmt.Errorf("pick a schema or a connection to compare with"))
		}
		connect, byName := w.a.d.Connect, w.a.connByName
		var text string
		var nstmts, ndrops int
		return w.job("schema diff", func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			prog := func(side string) func(done, total int) {
				return func(done, total int) {
					report(func() tea.Cmd { return w.a.say("schema diff: reading %s, table %d of %d", side, done+1, total) })
				}
			}
			other := sess
			if with != others[0] {
				c, _ := byName(with)
				o, err := connect(ctx, c)
				if err != nil {
					return err
				}
				defer o.Close()
				other = o.Sess
				if other.Caps().Editor != "sql" || other.Caps().Dialect != sess.Caps().Dialect {
					return fmt.Errorf("%s is not the same SQL dialect as %s", with, w.conn.Name)
				}
				if to == "" {
					to = other.CurrentSchema()
				}
			}
			slow := func(err error) error {
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("timed out after 10 minutes reading the schemas")
				}
				return err
			}
			a, err := tableDefs(ctx, sess, from, prog("FROM"))
			if err != nil {
				return slow(err)
			}
			b, err := tableDefs(ctx, other, to, prog("TO"))
			if err != nil {
				return slow(err)
			}
			for i := range b {
				b[i].Schema = from // the statements run against FROM, so a table is the same table in both
			}
			var quote []func(string) string
			if d, ok := db.DialectFor(sess.Caps().Dialect); ok {
				quote = append(quote, d.Quote)
			}
			keep, drops, err := tools.SchemaDiffParts(a, b, sess.PlanTable, quote...)
			if err != nil {
				return err
			}
			text, nstmts, ndrops = schemaDiffText(keep, drops, withDrops), len(keep), len(drops)
			if withDrops {
				nstmts += ndrops
			}
			return nil
		}, func() (string, tea.Cmd) {
			if nstmts == 0 && ndrops == 0 {
				return "the schemas are the same", nil
			}
			return fmt.Sprintf("schema diff: %s written to a new editor tab", plural(nstmts+ndrops, "statement")), w.newQueryTab(text)
		})
	}))
	return nil
}

// schemaDiffText is the editor text of a schema diff: a header with the counts, the changes, then the
// DROP TABLE statements (live when withDrops, otherwise commented out so running the tab cannot lose a table).
func schemaDiffText(keep, drops []db.Statement, withDrops bool) string {
	var sb strings.Builder
	write := func(prefix string, sts []db.Statement) {
		for _, s := range sts {
			sb.WriteString(prefix + strings.TrimRight(s.Shown(), "; \n") + ";\n\n")
		}
	}
	fmt.Fprintf(&sb, "-- schema diff: %s", plural(len(keep), "statement"))
	if len(drops) > 0 {
		if withDrops {
			fmt.Fprintf(&sb, ", plus %s: they DELETE DATA", plural(len(drops), "DROP TABLE"))
		} else {
			fmt.Fprintf(&sb, "; %s are commented out below (tables only in FROM): remove the leading -- to drop them", plural(len(drops), "DROP TABLE"))
		}
	}
	sb.WriteString("\n")
	write("", keep)
	if withDrops {
		write("", drops)
	} else {
		write("-- ", drops)
	}
	return sb.String()
}

// tableChoices lists the sidebar's tables as "schema.name" labels.
func (w *Workspace) tableChoices() ([]string, map[string]db.ObjRef) {
	var names []string
	refs := map[string]db.ObjRef{}
	for _, o := range w.sb.objs {
		if o.Kind != db.KindTable {
			continue
		}
		n := o.Name
		if o.Schema != "" {
			n = o.Schema + "." + o.Name
		}
		names = append(names, n)
		refs[n] = o.ObjRef
	}
	return names, refs
}

// dataDiff compares two tables with the same key and previews the changes that turn FROM into TO.
// shortcut: each side is read whole up to the connection's row limit; a bigger table is refused, not cut.
func (w *Workspace) dataDiff() tea.Cmd {
	names, refs := w.tableChoices()
	if len(names) < 2 {
		return w.a.say("data diff needs two tables in the sidebar")
	}
	w.a.push(newForm("Data diff: turn FROM into TO (same columns, same key)", []formField{
		{Key: "from", Label: "FROM table", Kind: fChoice, Options: names, Value: names[0]},
		{Key: "to", Label: "TO table", Kind: fChoice, Options: names, Value: names[1]},
		{Key: "pk", Label: "Key columns", Kind: fText, Hint: "empty: FROM's primary key"},
	}, func(v map[string]string) tea.Cmd {
		from, to := refs[v["from"]], refs[v["to"]]
		if from == to {
			return w.a.fail(fmt.Errorf("pick two different tables"))
		}
		var pk []string
		for _, c := range strings.Split(v["pk"], ",") {
			if c = strings.TrimSpace(c); c != "" {
				pk = append(pk, c)
			}
		}
		var stmts []db.Statement
		return w.job("data diff", func(ctx context.Context, sess db.Session, report func(func() tea.Cmd)) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			fc, err := sess.Columns(ctx, from)
			if err != nil {
				return err
			}
			tc, err := sess.Columns(ctx, to)
			if err != nil {
				return err
			}
			if len(pk) == 0 {
				for _, c := range fc {
					if c.PK {
						pk = append(pk, c.Name)
					}
				}
			}
			in := map[string]bool{}
			for _, c := range tc {
				in[c.Name] = true
			}
			var cols []string
			for _, c := range fc {
				if in[c.Name] {
					cols = append(cols, c.Name)
				}
			}
			if len(cols) == 0 {
				return fmt.Errorf("the tables have no column in common")
			}
			limit := w.a.d.State.RowLimit(w.conn.Name, w.a.d.Cfg.General.RowLimit) // same limit as the web
			var sorts []db.Sort
			for _, k := range pk {
				sorts = append(sorts, db.Sort{Column: k})
			}
			read := func(ref db.ObjRef) (*db.Result, error) {
				report(func() tea.Cmd { return w.a.say("data diff: reading %s…", ref.Name) })
				q := db.RowQuery{ObjRef: ref, Sort: sorts, Columns: cols}
				if limit > 0 {
					q.Limit = limit + 1 // one extra row tells a cut-off table from an exact fit
				}
				r, err := sess.Rows(ctx, q)
				if err == nil && limit > 0 && len(r.Rows) > limit {
					err = fmt.Errorf("%s has more than %d rows (the row limit): raise it to diff", ref.Name, limit)
				}
				return r, err
			}
			a, err := read(from)
			if err != nil {
				return err
			}
			b, err := read(to)
			if err != nil {
				return err
			}
			changes, err := tools.DataDiffChecked(a, b, pk)
			if err != nil {
				return err
			}
			for i := range changes {
				changes[i].ObjRef = from
			}
			if len(changes) > 0 {
				stmts, err = sess.Plan(changes)
			}
			return err
		}, func() (string, tea.Cmd) {
			if len(stmts) == 0 {
				return "nothing to change: the two sides are the same", nil
			}
			return "data diff: " + plural(len(stmts), "statement"), w.previewStatements("Data diff: "+from.Name+" -> "+to.Name, stmts, nil)
		})
	}))
	return nil
}

// previewStatements lists stmts in a text view with `a` to send them through the safe-mode gate.
func (w *Workspace) previewStatements(title string, stmts []db.Statement, then func(err error) tea.Cmd) tea.Cmd {
	if len(stmts) == 0 {
		return w.a.say("nothing to change: the two sides are the same")
	}
	var lines []string
	for _, s := range stmts {
		lines = append(lines, s.Shown())
	}
	w.a.push(&textViewDlg{title: fmt.Sprintf("%s (%s)", title, plural(len(stmts), "statement")), lines: lines, actions: []viewAction{
		{Key: "a", Label: "a: apply", Do: func() tea.Cmd { return w.confirmBulk(title, stmts, func() tea.Cmd { return w.execute(stmts, then) }) }},
	}})
	return nil
}

// bulkThreshold is the statement count from which applying a batch asks for a typed confirmation.
const bulkThreshold = 100

// confirmBulk runs apply at once for a small, non-destructive batch; a batch with DELETE, DROP or
// TRUNCATE, or more than bulkThreshold statements, first asks the user to type the statement count.
func (w *Workspace) confirmBulk(title string, stmts []db.Statement, apply func() tea.Cmd) tea.Cmd {
	destructive := 0
	for _, s := range stmts {
		switch f := strings.Fields(strings.ToUpper(s.Text)); {
		case len(f) > 0 && (f[0] == "DELETE" || f[0] == "DROP" || f[0] == "TRUNCATE"):
			destructive++
		}
	}
	if destructive == 0 && len(stmts) <= bulkThreshold {
		return apply()
	}
	want := fmt.Sprint(len(stmts))
	warn := "A large batch: check the preview before applying."
	if destructive > 0 {
		warn = fmt.Sprintf("%s delete rows or tables. This cannot be undone.", plural(destructive, "statement"))
	}
	f := newForm("Apply "+plural(len(stmts), "statement")+"?", []formField{
		{Kind: fLabel, Value: title},
		{Kind: fLabel, Value: warn},
		{Key: "confirm", Label: "Type " + want + " to confirm", Kind: fText},
	}, func(map[string]string) tea.Cmd { return apply() })
	f.validate = func(v map[string]string) error {
		if strings.TrimSpace(v["confirm"]) != want {
			return fmt.Errorf("type %s to confirm", want)
		}
		return nil
	}
	f.submit = "Enter: apply"
	w.a.push(f)
	return nil
}

// bulkUpdate is the bulk_update action on a filtered grid: one UPDATE for every row the filter matches.
func (t *tableTab) bulkUpdate() tea.Cmd {
	applied := t.applied()
	if len(applied.Active()) == 0 {
		return t.ws.a.say("filter the rows first: a bulk update changes every row the filter matches")
	}
	if !t.grid.Editable {
		return t.ws.a.say("%s is read-only", t.ref.Name)
	}
	d, ok := db.DialectFor(t.ws.caps.Dialect)
	if !ok {
		return t.ws.a.say("bulk update is for SQL engines")
	}
	f := newForm(fmt.Sprintf("Bulk update %s where %d filter(s) match", t.ref.Name, len(applied.Active())), []formField{
		{Key: "set", Label: "Set", Kind: fText, Hint: "col=value ;; col2=NULL ;; col3==expr"},
	}, func(v map[string]string) tea.Cmd {
		set, err := parseAssignments(v["set"])
		if err != nil {
			return t.ws.a.fail(err)
		}
		var cols []string
		for _, c := range t.cols {
			cols = append(cols, c.Name)
		}
		st, err := db.BulkUpdate(d, t.ref, applied, set, cols)
		if err != nil {
			return t.ws.a.fail(err)
		}
		return t.ws.previewStatements("Bulk update "+t.ref.Name, []db.Statement{st}, func(err error) tea.Cmd {
			if err != nil || t.closed {
				return nil
			}
			return t.load()
		})
	})
	f.validate = func(v map[string]string) error { _, err := parseAssignments(v["set"]); return err }
	t.ws.a.push(f)
	return nil
}

// parseAssignments reads "a=1 ;; b=NULL ;; c==now()" (NULL is SQL NULL, a second = starts an expression).
func parseAssignments(s string) ([]db.KeyVal, error) {
	var out []db.KeyVal
	for _, part := range strings.Split(s, ";;") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		k, val, ok := strings.Cut(part, "=")
		if k = strings.TrimSpace(k); !ok || k == "" {
			return nil, fmt.Errorf("%q is not column=value", strings.TrimSpace(part))
		}
		v := toolValue(strings.TrimSpace(val))
		if e, ok := v.(db.Expr); ok {
			if err := checkExpr(string(e)); err != nil {
				return nil, err
			}
		}
		out = append(out, db.KeyVal{Col: k, Val: v})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("give at least one column=value")
	}
	return out, nil
}

// fakeRows inserts n generated rows into a table (the same seed gives the same rows).
func (w *Workspace) fakeRows() tea.Cmd {
	names, refs := w.tableChoices()
	if len(names) == 0 {
		return w.a.say("no tables to fill")
	}
	def := names[0]
	if r, ok := w.targetRef(); ok {
		for n, ref := range refs {
			if ref == r {
				def = n
			}
		}
	}
	w.a.push(newForm("Fake data", []formField{
		{Key: "table", Label: "Table", Kind: fChoice, Options: names, Value: def},
		{Key: "n", Label: "Rows", Kind: fText, Value: "10"},
		{Key: "seed", Label: "Seed", Kind: fText, Value: "1"},
	}, func(v map[string]string) tea.Cmd {
		n, err := strconv.Atoi(strings.TrimSpace(v["n"]))
		seed, err2 := strconv.ParseInt(strings.TrimSpace(v["seed"]), 10, 64)
		if err != nil || err2 != nil || n < 1 || n > maxFakeRows {
			return w.a.fail(fmt.Errorf("rows must be 1 to %d and the seed a number", maxFakeRows))
		}
		ref, sess := refs[v["table"]], w.sess
		return liveCmd(func() func() tea.Cmd {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cols, err := sess.Columns(ctx, ref)
			var stmts []db.Statement
			if err == nil {
				changes := tools.FakeRows(cols, n, seed)
				for i := range changes {
					changes[i].ObjRef = ref
				}
				stmts, err = sess.Plan(changes)
			}
			return func() tea.Cmd {
				if err != nil {
					return w.a.fail(err)
				}
				return w.previewStatements("Fake data for "+ref.Name, stmts, func(err error) tea.Cmd {
					if err != nil {
						return nil
					}
					w.forget(&ref)
					return tea.Batch(w.a.say("inserted %s into %s", plural(n, "row"), ref.Name), w.reloadTab(), w.sb.load())
				})
			}
		})
	}))
	return nil
}
