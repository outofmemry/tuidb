package mysql

import (
	"errors"
	"reflect"
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
		{db.UserChange{Kind: "create", User: db.User{Name: "bob", Host: "localhost"}, Password: `p'w`},
			"CREATE USER 'bob'@'localhost' IDENTIFIED BY 'p''w'", "CREATE USER 'bob'@'localhost' IDENTIFIED BY '********'"},
		{db.UserChange{Kind: "password", User: db.User{Name: "a"}, Password: "x"}, "ALTER USER 'a'@'%' IDENTIFIED BY 'x'", "ALTER USER 'a'@'%' IDENTIFIED BY '********'"},
		{db.UserChange{Kind: "drop", User: db.User{Name: "a", Host: "h"}}, "DROP USER 'a'@'h'", ""},
		{db.UserChange{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"all"}}}, "GRANT ALL PRIVILEGES ON *.* TO 'a'@'%'", ""},
		{db.UserChange{Kind: "limits", User: db.User{Name: "a", Limits: map[string]int{"MAX_USER_CONNECTIONS": 3, "MAX_QUERIES_PER_HOUR": 100, "BOGUS; DROP": 1}}},
			"ALTER USER 'a'@'%' WITH MAX_QUERIES_PER_HOUR 100 MAX_USER_CONNECTIONS 3", ""},
		{db.UserChange{Kind: "grant", User: db.User{Name: "a"}, Grant: db.Grant{Object: "d`b.*", Privs: []string{"select", "insert"}}},
			"GRANT SELECT, INSERT ON `d``b`.* TO 'a'@'%'", ""},
		{db.UserChange{Kind: "revoke", User: db.User{Name: "a"}, Grant: db.Grant{Object: "d.t", Privs: []string{"SELECT"}}},
			"REVOKE SELECT ON `d`.`t` FROM 'a'@'%'", ""},
	}
	for _, c := range cases {
		st, err := s.PlanUser(c.c)
		if err != nil || len(st) != 1 || st[0].Text != c.want || (c.disp != "" && st[0].Display != c.disp) {
			t.Errorf("%s: %+v %v", c.c.Kind, st, err)
		}
	}
	if _, err := s.PlanUser(db.UserChange{Kind: "grant", User: db.User{Name: "a"}, Grant: db.Grant{Object: "d.*", Privs: []string{"X;"}}}); err == nil {
		t.Error("injection accepted")
	}
}

func TestAddGrant(t *testing.T) {
	var u db.User
	for _, l := range []string{"GRANT USAGE ON *.* TO `a`@`%`", "GRANT SELECT, INSERT ON *.* TO `a`@`%`",
		"GRANT ALL PRIVILEGES ON `d`.* TO `a`@`%`", "GRANT `role` TO `a`@`%`"} {
		addGrant(&u, l)
	}
	want := db.User{Attrs: []string{"SELECT", "INSERT"}, Grants: []db.Grant{{Object: "d.*", Privs: []string{"ALL"}}}}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("%+v", u)
	}
	s := &session{}
	for _, c := range []db.UserChange{
		{Kind: "password", User: db.User{Name: "root", Host: "%"}},
		{Kind: "create", User: db.User{Name: "a"}},
		{Kind: "create", User: db.User{Name: "a"}, Password: `x\`},
		{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"BINLOG_ADMIN; DROP"}}},
	} {
		if _, err := s.PlanUser(c); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	if _, err := s.PlanUser(db.UserChange{Kind: "attrs", User: db.User{Name: "a", Attrs: []string{"BINLOG_ADMIN"}}}); err != nil {
		t.Error(err)
	}
}

func TestColumnGrants(t *testing.T) {
	var u db.User
	addGrant(&u, "GRANT SELECT, INSERT (`a`, b), UPDATE (c) ON `d`.`t` TO `u`@`%`")
	want := db.User{Grants: []db.Grant{{Object: "d.t", Privs: []string{"SELECT"}},
		{Object: "d.t", Privs: []string{"INSERT"}, Columns: []string{"a", "b"}}, {Object: "d.t", Privs: []string{"UPDATE"}, Columns: []string{"c"}}}}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("%+v", u)
	}
	st, err := (&session{}).PlanUser(db.UserChange{Kind: "grant", User: db.User{Name: "u", Host: "h"}, Grant: db.Grant{Object: "d.t", Privs: []string{"select", "update"}, Columns: []string{"a", "b"}}})
	if err != nil || st[0].Text != "GRANT SELECT (`a`, `b`), UPDATE (`a`, `b`) ON `d`.`t` TO 'u'@'h'" {
		t.Errorf("%v %v", st, err)
	}
}

func TestSchemaOpRefused(t *testing.T) {
	if _, err := (&session{}).PlanAdmin(db.OpCreateSchema, db.ObjRef{Schema: "x"}, nil); err == nil {
		t.Error("mysql schema = database")
	}
}

type fakeLimitRows struct {
	n       int
	scanErr bool
}

func (f *fakeLimitRows) Next() bool { f.n++; return f.n <= 2 }
func (f *fakeLimitRows) Err() error { return nil }
func (f *fakeLimitRows) Scan(d ...any) error {
	if f.scanErr && f.n == 2 {
		return errors.New("boom")
	}
	*d[0].(*string), *d[1].(*string) = "a", "%"
	return nil
}

func TestScanLimitsNeverHalfFilled(t *testing.T) {
	us := []db.User{{Name: "a", Host: "%"}, {Name: "b", Host: "%"}}
	scanLimits(&fakeLimitRows{scanErr: true}, us)
	if us[0].Limits != nil || us[1].Limits != nil {
		t.Errorf("a failed read left limits: %+v", us)
	}
	us = []db.User{{Name: "a", Host: "%"}, {Name: "b", Host: "%"}}
	scanLimits(&fakeLimitRows{}, us)
	if us[0].Limits == nil || us[1].Limits != nil {
		t.Errorf("a clean read: %+v", us)
	}
}
