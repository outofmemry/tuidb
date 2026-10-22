package db

import (
	"errors"
	"fmt"
	"strings"
)

// PlanSQL turns pending GUI edits into parameterized statements for the SQL dialects.
// Text uses placeholders; Display inlines literals for Preview, history and the console.
func PlanSQL(d Dialect, changes []Change) ([]Statement, error) {
	var out []Statement
	for _, c := range changes {
		st, err := planOne(d, c)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// builder accumulates a statement's parameterized and display forms together.
type builder struct {
	d    Dialect
	text strings.Builder
	disp strings.Builder
	args []any
}

func (b *builder) raw(s string) { b.text.WriteString(s); b.disp.WriteString(s) }

// val writes a value: keywords/expressions inline, NULL inline, everything else as a parameter.
func (b *builder) val(v any) {
	switch x := v.(type) {
	case nil:
		b.raw("NULL")
	case special:
		switch x {
		case Null:
			b.raw("NULL")
		case Default:
			b.raw("DEFAULT")
		default:
			b.arg("")
		}
	case Expr:
		b.raw(string(x))
	default:
		b.arg(v)
	}
}

func (b *builder) arg(v any) {
	b.args = append(b.args, v)
	b.text.WriteString(b.d.Param(len(b.args)))
	b.disp.WriteString(b.d.Literal(v))
}

func (b *builder) where(key []KeyVal) {
	b.raw(" WHERE ")
	for i, k := range key {
		if i > 0 {
			b.raw(" AND ")
		}
		b.raw(b.d.Quote(k.Col))
		if IsNull(k.Val) {
			b.raw(" IS NULL")
			continue
		}
		b.raw(" = ")
		b.arg(k.Val)
	}
}

func planOne(d Dialect, c Change) (Statement, error) {
	b := &builder{d: d}
	tbl := d.QualifiedName(c.ObjRef)
	switch c.Kind {
	case Insert:
		var cols []KeyVal
		for _, kv := range c.Set {
			if kv.Val == Default && d.Mutate {
				continue // ClickHouse has no DEFAULT keyword in VALUES: omit the column
			}
			cols = append(cols, kv)
		}
		switch {
		case len(cols) == 0 && d.Name == "postgres":
			b.raw("INSERT INTO " + tbl + " DEFAULT VALUES")
		case len(cols) == 0:
			b.raw("INSERT INTO " + tbl + " () VALUES ()")
		default:
			names := make([]string, len(cols))
			for i, kv := range cols {
				names[i] = d.Quote(kv.Col)
			}
			b.raw("INSERT INTO " + tbl + " (" + strings.Join(names, ", ") + ") VALUES (")
			for i, kv := range cols {
				if i > 0 {
					b.raw(", ")
				}
				b.val(kv.Val)
			}
			b.raw(")")
		}
	case Update:
		if len(c.Set) == 0 {
			return Statement{}, errors.New("update without changed columns")
		}
		if len(c.Key) == 0 {
			return Statement{}, fmt.Errorf("cannot update %s: no primary key and no original values to identify the row", c.Name)
		}
		if d.Mutate {
			b.raw("ALTER TABLE " + tbl + " UPDATE ")
		} else {
			b.raw("UPDATE " + tbl + " SET ")
		}
		for i, kv := range c.Set {
			if i > 0 {
				b.raw(", ")
			}
			b.raw(d.Quote(kv.Col) + " = ")
			b.val(kv.Val)
		}
		b.where(c.Key)
	case Delete:
		if len(c.Key) == 0 {
			return Statement{}, fmt.Errorf("cannot delete from %s: no primary key and no original values to identify the row", c.Name)
		}
		if d.Mutate {
			b.raw("ALTER TABLE " + tbl + " DELETE")
		} else {
			b.raw("DELETE FROM " + tbl)
		}
		b.where(c.Key)
	default:
		return Statement{}, fmt.Errorf("unknown change kind %d", c.Kind)
	}
	return Statement{Text: b.text.String(), Args: b.args, Display: b.disp.String()}, nil
}
