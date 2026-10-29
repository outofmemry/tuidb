package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestM13MySQLLimitsChange(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%", Limits: map[string]int{"MAX_USER_CONNECTIONS": 2}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	h.want("Resource limits of ann@%")
	setField(t, h, "MAX_QUERIES_PER_HOUR", "500")
	h.press("enter")
	var got *db.UserChange
	for i := range h.fake.userChanges {
		if h.fake.userChanges[i].Kind == "limits" {
			got = &h.fake.userChanges[i]
		}
	}
	if got == nil || got.User.Limits["MAX_QUERIES_PER_HOUR"] != 500 || got.User.Limits["MAX_USER_CONNECTIONS"] != 2 {
		t.Fatalf("changes %+v", h.fake.userChanges)
	}
}

func TestM13PostgresConnectionLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Limits: map[string]int{"CONNECTION_LIMIT": -1}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Connection limit")
	h.want("Connection limit of ann")
	setField(t, h, "n", "7")
	h.press("enter")
	var got *db.UserChange
	for i := range h.fake.userChanges {
		if h.fake.userChanges[i].Kind == "limits" {
			got = &h.fake.userChanges[i]
		}
	}
	if got == nil || got.User.Limits["CONNECTION_LIMIT"] != 7 {
		t.Fatalf("changes %+v", h.fake.userChanges)
	}
}

// P2-69 / C1: unreadable limits refuse the form; MAX_USER_CONNECTIONS says 0 is the server default.
func TestTUI1MySQLLimitsUnreadableAndHints(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%"}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	h.want("resource limits could not be read")
	if n := len(h.a.overlays); n > 0 {
		if _, ok := h.a.overlays[n-1].(*formDlg); ok {
			t.Fatal("the form must not open")
		}
	}

	h = newHarness(t, nil)
	h.fake.dialect = "mysql"
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Host: "%", Limits: map[string]int{}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Resource limits")
	for _, fl := range h.a.overlays[len(h.a.overlays)-1].(*formDlg).fields {
		want := "0 = unlimited"
		if fl.Key == "MAX_USER_CONNECTIONS" {
			want = "0 = the server default"
		}
		if fl.Hint != want {
			t.Fatalf("%s hint %q", fl.Key, fl.Hint)
		}
	}
}

func TestM3UsersCreateGoesThroughPreview(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann", Attrs: []string{"LOGIN"}}}
	h.connect()
	h.prefix("u")
	h.want("Users (1)")
	h.want("ann")
	h.press("enter") // "New user"
	setField(t, h, "name", "bob")
	setField(t, h, "pw", "s3cret")
	h.press("enter")
	h.want("Attributes for bob")
	h.press("enter")
	h.want("Preview: 1 statement")
	h.want("CREATE ROLE bob PASSWORD '********'")
	h.wantNot("s3cret")
	if len(h.fake.applied) != 0 {
		t.Fatal("nothing may be sent before the preview is accepted")
	}
	h.press("enter")
	if len(h.fake.applied) != 1 || !strings.Contains(h.fake.applied[0][0].Text, "'s3cret'") {
		t.Fatalf("applied %+v", h.fake.applied)
	}
	h.want("Users (1)") // the list comes back
}

func TestM3UsersDropNeedsTheName(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn, h.fake.ulist = true, []db.User{{Name: "ann"}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter") // ann
	h.pickLabel("Drop user")
	h.want("Drop user ann?")
	h.press("enter")
	h.want("type “ann”")
	setField(t, h, "confirm", "ann")
	h.press("enter", "enter")
	if len(h.fake.applied) != 1 || h.fake.applied[0][0].Text != "DROP ann" {
		t.Fatalf("applied %+v", h.fake.applied)
	}
}

func TestM3UsersNotSupported(t *testing.T) {
	h := newHarness(t, nil)
	h.connect()
	h.prefix("u")
	h.want("not supported by postgres")
}

func TestM7ColumnLevelGrants(t *testing.T) {
	h := newHarness(t, nil)
	h.fake.usersOn = true
	h.fake.ulist = []db.User{{Name: "ann", Grants: []db.Grant{{Object: "public.customers", Privs: []string{"SELECT"}, Columns: []string{"id", "name"}}}}}
	h.connect()
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Details")
	h.want("SELECT (id, name)")
	h.press("esc")
	h.prefix("u")
	h.press("down", "enter")
	h.pickLabel("Grant privileges")
	setField(t, h, "cols", "email, name")
	h.press("enter")
	h.press("enter") // apply the preview; the user list comes back
	last := h.fake.userChanges[len(h.fake.userChanges)-1]
	if last.Kind != "grant" || strings.Join(last.Grant.Columns, ",") != "email,name" {
		t.Fatalf("change %+v", last)
	}
	h.press("down", "enter")
	h.pickLabel("Revoke privileges")
	h.want("SELECT (id, name)")
	h.press("enter")
	if f := h.a.overlays[len(h.a.overlays)-1].(*formDlg); f.values()["cols"] != "id, name" {
		t.Fatalf("revoke form %v", f.values())
	}
}
