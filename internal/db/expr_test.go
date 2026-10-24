package db

import "testing"

func TestCheckExpr(t *testing.T) {
	if CheckExpr("upper(name) || 'a;b'") != nil {
		t.Error("a semicolon inside a string is fine")
	}
	for _, e := range []string{"1; DROP TABLE t", "1 -- x", "(1", ""} {
		if CheckExpr(e) == nil {
			t.Errorf("%q must be refused", e)
		}
	}
}
