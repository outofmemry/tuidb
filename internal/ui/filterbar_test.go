package ui

import (
	"testing"

	"webtuidb/internal/db"
)

func TestInlineArgsKeepsOrderForQuestionMarkDialects(t *testing.T) {
	d, _ := db.DialectFor("mysql")
	if got := inlineArgs(d, "`a` = ? AND `b` = ?", []any{1, 2}); got != "`a` = "+d.Literal(1)+" AND `b` = "+d.Literal(2) {
		t.Errorf("mysql: %s", got)
	}
	p, _ := db.DialectFor("postgres")
	if got := inlineArgs(p, `"a" = $1 AND "b" = $2`, []any{"x?", "y"}); got != `"a" = `+p.Literal("x?")+` AND "b" = `+p.Literal("y") {
		t.Errorf("postgres: %s", got)
	}
}
