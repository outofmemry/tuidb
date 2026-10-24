package postgres

import (
	"testing"

	"webtuidb/internal/db"
)

func TestPlanUser(t *testing.T) {
	s := &session{}
	cases := []struct {
		c    db.UserChange
		want string
		disp string
	}{
		{db.UserChange{Kind: "create", User: db.User{Name: "bo\"b", Attrs: []string{"login", "createdb"}}, Password: "p'w"},
			`CREATE ROLE "bo""b" WITH LOGIN CREATEDB PASSWORD E'p''w'`, `CREATE ROLE "bo""b" WITH LOGIN CREATEDB PASSWORD '********'`},
		{db.UserChange{Kind: "password", User: db.User{Name: "a"}, Password: "x"}, `ALTER ROLE "a" PASSWORD E'x'`, `ALTER ROLE "a" PASSWORD '********'`},
		{db.UserChange{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"NOSUPERUSER"}}}, `ALTER ROLE "a" WITH NOSUPERUSER`, ""},
		{db.UserChange{Kind: "drop", User: db.User{Name: "a"}}, `DROP ROLE "a"`, ""},
		{db.UserChange{Kind: "grant", User: db.User{Name: "a"}, Grant: db.Grant{Object: "s.t", Privs: []string{"select", "insert"}}},
			`GRANT SELECT, INSERT ON TABLE "s"."t" TO "a"`, ""},
		{db.UserChange{Kind: "revoke", User: db.User{Name: "a"}, Grant: db.Grant{Object: "t", Privs: []string{"SELECT"}}},
			`REVOKE SELECT ON TABLE "public"."t" FROM "a"`, ""},
	}
	for _, c := range cases {
		st, err := s.PlanUser(c.c)
		if err != nil || len(st) != 1 || st[0].Text != c.want || (c.disp != "" && st[0].Display != c.disp) {
			t.Errorf("%s: %+v %v", c.c.Kind, st, err)
		}
	}
	if _, err := s.PlanUser(db.UserChange{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"X; DROP"}}}); err == nil {
		t.Error("injection accepted")
	}
	for _, c := range []db.UserChange{
		{Kind: "password", User: db.User{Name: "a"}},
		{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"LOGIN IN ROLE SOMEROLE"}}},
	} {
		if _, err := s.PlanUser(c); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	if st, _ := s.PlanUser(db.UserChange{Kind: "password", User: db.User{Name: "a"}, Password: `x\'; --`}); st[0].Text != `ALTER ROLE "a" PASSWORD E'x\\''; --'` {
		t.Errorf("escape: %s", st[0].Text)
	}
}

func TestColumnGrant(t *testing.T) {
	st, err := (&session{}).PlanUser(db.UserChange{Kind: "revoke", User: db.User{Name: "r"}, Grant: db.Grant{Object: "s.t", Privs: []string{"select", "update"}, Columns: []string{"a", "b"}}})
	if err != nil || st[0].Text != `REVOKE SELECT ("a", "b"), UPDATE ("a", "b") ON TABLE "s"."t" FROM "r"` {
		t.Errorf("%v %v", st, err)
	}
}

func TestSchemaAdmin(t *testing.T) {
	s := &session{}
	for op, want := range map[db.AdminOp]string{db.OpCreateSchema: `CREATE SCHEMA "x"`, db.OpDropSchema: `DROP SCHEMA "x" CASCADE`} {
		st, err := s.PlanAdmin(op, db.ObjRef{Schema: "x"}, map[string]string{"cascade": "true"})
		if op == db.OpCreateSchema {
			st, err = s.PlanAdmin(op, db.ObjRef{Name: "x"}, nil)
		}
		if err != nil || st[0].Text != want {
			t.Errorf("%s: %v %v", op, st, err)
		}
	}
}
