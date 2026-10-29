package web

import (
	"testing"
)

func TestExprNeedsMacroOrAdvanced(t *testing.T) {
	var v Val
	if err := v.UnmarshalJSON([]byte(`{"$expr":"(select pg_sleep(300))"}`)); err == nil {
		t.Fatal("arbitrary expression accepted")
	}
	if err := v.UnmarshalJSON([]byte(`{"$expr":"(select 1)","$advanced":"true"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.UnmarshalJSON([]byte(`{"$expr":"now()"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckExpr(t *testing.T) {
	for _, ok := range []string{"NOW()", "now() + interval '1 day'", "nextval('public.seq'::regclass)", "'it''s;fine'", `"a;b"`, "gen_random_uuid()"} {
		if err := checkExpr(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1); DROP TABLE t; --", "1; select 2", "a -- b", "a /* b */", "(1", "'open", "$$x$$"} {
		if checkExpr(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
