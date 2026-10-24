package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"tuidb/internal/db"
)

func (s *session) Databases(ctx context.Context) ([]string, error) {
	rows, err := s.db().QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.schemata ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Objects ignores schema: in MySQL the database is the schema.
func (s *session) Objects(ctx context.Context, _ string) ([]db.Object, error) {
	cur := s.CurrentDatabase()
	if cur == "" {
		return nil, fmt.Errorf("no database selected")
	}
	rows, err := s.db().QueryContext(ctx, "SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS FROM information_schema.tables WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", cur)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var out []db.Object
	versioned := map[string]bool{}
	for rows.Next() {
		var name, typ string
		var n sql.NullInt64
		if err := rows.Scan(&name, &typ, &n); err != nil {
			return nil, err
		}
		versioned[cur+"."+name] = typ == "SYSTEM VERSIONED"
		o := db.Object{ObjRef: db.ObjRef{Name: name, Kind: db.KindTable}}
		if strings.Contains(typ, "VIEW") {
			o.Kind = db.KindView
		} else if typ == "SEQUENCE" && s.caps.Sequences { // MariaDB; the sequence is a one-row table
			o.Kind = db.KindSequence
		} else if n.Valid {
			o.Detail = strconv.FormatInt(n.Int64, 10)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.versioned = versioned
	s.mu.Unlock()
	rr, err := s.db().QueryContext(ctx, "SELECT ROUTINE_NAME, ROUTINE_TYPE FROM information_schema.routines WHERE ROUTINE_SCHEMA = ? ORDER BY ROUTINE_TYPE DESC, ROUTINE_NAME", cur)
	if err != nil {
		return nil, fmt.Errorf("list routines: %w", err)
	}
	defer rr.Close()
	for rr.Next() {
		var name, typ string
		if err := rr.Scan(&name, &typ); err != nil {
			return nil, err
		}
		k := db.KindFunction
		if typ == "PROCEDURE" {
			k = db.KindProcedure
		}
		out = append(out, db.Object{ObjRef: db.ObjRef{Name: name, Kind: k}})
	}
	return out, rr.Err()
}

func (s *session) loadColumns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	// The collation is only loaded where it differs from the table's: MODIFY without one keeps the table default.
	// MariaDB's JSON is LONGTEXT COLLATE utf8mb4_bin plus CHECK (json_valid(col)); a MODIFY that spells it
	// as longtext would silently drop the check, so such columns are reported as json.
	isJSON := "0"
	if s.maria {
		isJSON = "(c.DATA_TYPE = 'longtext' AND c.COLLATION_NAME = 'utf8mb4_bin' AND EXISTS (SELECT 1 FROM information_schema.check_constraints k " +
			"WHERE k.CONSTRAINT_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME AND k.CHECK_CLAUSE = CONCAT('json_valid(`', c.COLUMN_NAME, '`)')))"
	}
	rows, err := s.db().QueryContext(ctx, `SELECT c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE, c.COLUMN_DEFAULT, c.EXTRA, c.COLUMN_KEY, c.DATA_TYPE,
			c.COLUMN_COMMENT, IF(c.COLLATION_NAME <> t.TABLE_COLLATION, c.COLLATION_NAME, NULL), c.GENERATION_EXPRESSION, `+isJSON+`, t.TABLE_TYPE
		FROM information_schema.columns c JOIN information_schema.tables t ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
		WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME = ? ORDER BY c.ORDINAL_POSITION`, s.schemaOf(ref), ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()
	var out []db.Column
	for rows.Next() {
		var name, typ, nullable, extra, key, dataType, comment, tableType string
		var def, coll, gen sql.NullString
		var json bool
		if err := rows.Scan(&name, &typ, &nullable, &def, &extra, &key, &dataType, &comment, &coll, &gen, &json, &tableType); err != nil {
			return nil, err
		}
		s.noteVersioned(s.schemaOf(ref), ref.Name, tableType == "SYSTEM VERSIONED")
		if json {
			typ, coll = "json", sql.NullString{}
		}
		c := db.Column{Name: name, OrigName: name, Type: typ, Nullable: nullable == "YES", PK: key == "PRI"}
		c.Default, c.HasDef = columnDefault(dataType, def, extra, s.maria)
		c.Extra = columnExtra(extra, gen.String, comment, s.maria)
		if coll.Valid { // part of the type so it stays right after it, where generated columns need it
			c.Type += " COLLATE " + coll.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *session) Columns(ctx context.Context, ref db.ObjRef) ([]db.Column, error) {
	cols, err := s.loadColumns(ctx, ref)
	if err != nil {
		return nil, err
	}
	fks, err := s.ForeignKeys(ctx, ref)
	if err != nil {
		return nil, err
	}
	for i := range fks {
		for j := range cols {
			for _, fc := range fks[i].Columns {
				if cols[j].Name == fc {
					cols[j].FK = &fks[i]
				}
			}
		}
	}
	return cols, nil
}

// columnExtra is the column's SQL after type, nullability and default: EXTRA (auto_increment,
// on update ...), the full GENERATED ALWAYS AS clause in place of EXTRA's bare "STORED GENERATED",
// and the comment, so a MODIFY/CHANGE rebuilt from it keeps all of them.
func columnExtra(extra, gen, comment string, maria bool) string {
	e := strings.TrimSpace(strings.ReplaceAll(extra, "DEFAULT_GENERATED", ""))
	if gen != "" {
		kind := "VIRTUAL"
		if strings.Contains(e, "STORED") || strings.Contains(e, "PERSISTENT") {
			kind = "STORED"
		}
		if !maria {
			gen = unescapeExpr(gen)
		}
		rest := strings.NewReplacer("VIRTUAL GENERATED", "", "STORED GENERATED", "", "PERSISTENT GENERATED", "").Replace(e)
		e = strings.TrimSpace("GENERATED ALWAYS AS (" + gen + ") " + kind + " " + strings.TrimSpace(rest))
	}
	if comment != "" {
		e = strings.TrimSpace(e + " COMMENT " + quote(comment))
	}
	return e
}

// unescapeExpr undoes MySQL 8's extra escaping of quotes in information_schema expressions
// (GENERATION_EXPRESSION, statistics.EXPRESSION show _utf8mb4\'x\' for _utf8mb4'x').
func unescapeExpr(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(s)
}

var rawDefault = map[string]bool{"tinyint": true, "smallint": true, "mediumint": true, "int": true, "bigint": true,
	"decimal": true, "numeric": true, "float": true, "double": true, "bit": true}

// columnDefault returns the default as SQL text. MySQL stores literal defaults unquoted and
// flags expression defaults with DEFAULT_GENERATED; MariaDB already stores SQL text.
func columnDefault(dataType string, def sql.NullString, extra string, maria bool) (string, bool) {
	if !def.Valid {
		return "", false
	}
	if maria {
		if def.String == "NULL" {
			return "", false
		}
		return def.String, true
	}
	if strings.Contains(extra, "DEFAULT_GENERATED") || rawDefault[strings.ToLower(dataType)] {
		return def.String, true
	}
	return quote(def.String), true
}

func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
}

func (s *session) Indexes(ctx context.Context, ref db.ObjRef) ([]db.Index, error) {
	expr := "NULL"
	if hasIndexExpr(s.info) {
		expr = "EXPRESSION"
	}
	rows, err := s.db().QueryContext(ctx, `SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, INDEX_TYPE, `+expr+` FROM information_schema.statistics
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME = 'PRIMARY' DESC, INDEX_NAME, SEQ_IN_INDEX`, s.schemaOf(ref), ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()
	var out []db.Index
	for rows.Next() {
		var name, algo string
		var nonUnique int
		var col, ex sql.NullString
		if err := rows.Scan(&name, &nonUnique, &col, &algo, &ex); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, db.Index{Name: name, Unique: nonUnique == 0, Primary: name == "PRIMARY", Algorithm: algo})
		}
		c := col.String
		if !col.Valid { // functional key part: kept as "(expr)", which indexSQL emits raw
			c = "(" + unescapeExpr(ex.String) + ")"
		}
		last := &out[len(out)-1]
		last.Columns = append(last.Columns, c)
	}
	return out, rows.Err()
}

// hasIndexExpr: statistics.EXPRESSION (functional key parts) exists since MySQL 8.0.13; MariaDB has neither.
func hasIndexExpr(info string) bool {
	v, ok := strings.CutPrefix(info, "MySQL ")
	if !ok {
		return false
	}
	var a, b, c int
	fmt.Sscanf(v, "%d.%d.%d", &a, &b, &c)
	return a > 8 || a == 8 && (b > 0 || c >= 13)
}

func (s *session) Triggers(ctx context.Context, ref db.ObjRef) ([]db.Trigger, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT TRIGGER_NAME, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT FROM information_schema.triggers
		WHERE EVENT_OBJECT_SCHEMA = ? AND EVENT_OBJECT_TABLE = ? ORDER BY TRIGGER_NAME`, s.schemaOf(ref), ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	defer rows.Close()
	var out []db.Trigger
	for rows.Next() {
		var t db.Trigger
		if err := rows.Scan(&t.Name, &t.Timing, &t.Event, &t.Statement); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *session) ForeignKeys(ctx context.Context, ref db.ObjRef) ([]db.ForeignKey, error) {
	cur := s.schemaOf(ref)
	rows, err := s.db().QueryContext(ctx, `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_SCHEMA, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE
		FROM information_schema.key_column_usage k
		JOIN information_schema.referential_constraints r ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME AND r.TABLE_NAME = k.TABLE_NAME
		WHERE k.TABLE_SCHEMA = ? AND k.TABLE_NAME = ? AND k.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`, cur, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("list foreign keys: %w", err)
	}
	defer rows.Close()
	var out []db.ForeignKey
	for rows.Next() {
		var name, col, refSchema, refTable, refCol, upd, del string
		if err := rows.Scan(&name, &col, &refSchema, &refTable, &refCol, &upd, &del); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			fk := db.ForeignKey{Name: name, RefTable: refTable, OnUpdate: upd, OnDelete: del}
			if refSchema != cur {
				fk.RefSchema = refSchema
			}
			out = append(out, fk)
		}
		last := &out[len(out)-1]
		last.Columns = append(last.Columns, col)
		last.RefColumns = append(last.RefColumns, refCol)
	}
	return out, rows.Err()
}

func (s *session) DDL(ctx context.Context, ref db.ObjRef) (string, error) {
	var kw string
	var idx int // position of the CREATE text in the SHOW CREATE row
	switch ref.Kind {
	case db.KindTable:
		kw, idx = "TABLE", 1
	case db.KindView:
		kw, idx = "VIEW", 1
	case db.KindSequence:
		kw, idx = "SEQUENCE", 1
	case db.KindFunction:
		kw, idx = "FUNCTION", 2
	case db.KindProcedure:
		kw, idx = "PROCEDURE", 2
	default:
		return "", fmt.Errorf("no DDL for %s %q", ref.Kind, ref.Name)
	}
	rows, err := s.db().QueryContext(ctx, "SHOW CREATE "+kw+" "+db.MySQL.QualifiedName(ref))
	if err != nil {
		return "", fmt.Errorf("show create %s: %w", strings.ToLower(kw), err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		return "", fmt.Errorf("%s %q not found", ref.Kind, ref.Name)
	}
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return "", err
	}
	if idx >= len(vals) || !vals[idx].Valid {
		return "", fmt.Errorf("server returned no definition for %s %q (missing privilege?)", ref.Kind, ref.Name)
	}
	return vals[idx].String + ";", nil
}
