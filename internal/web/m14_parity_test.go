package web

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"webtuidb/internal/db"
	"webtuidb/internal/menus"
)

var gatedCaps = []string{"Indexes", "Triggers", "Functions", "ProcessList", "Users", "Schemas", "Document", "Views", "MatViews", "Sequences", "ReorderColumns"}

// capsWith returns Capabilities with every gated flag on except (or only) name.
func capsWith(name string, on bool) db.Capabilities {
	c := db.Capabilities{Dialect: "postgres", Editor: "sql", Transactions: true}
	v := reflect.ValueOf(&c).Elem()
	for _, n := range gatedCaps {
		v.FieldByName(n).SetBool(!on)
	}
	v.FieldByName(name).SetBool(on)
	return c
}

// TestParityCapabilityGating (M14 check 7): the session reports each capability the client gates on, the gated routes
// refuse (or answer supported:false) with it off, and the TSX reads every capability.
func TestParityCapabilityGating(t *testing.T) {
	for _, name := range gatedCaps {
		for _, on := range []bool{true, false} {
			e := newEnv(t, "")
			name, on := name, on
			e.fake.capsFn = func() db.Capabilities { return capsWith(name, on) }
			id := e.open()
			info := e.ok("GET", "/api/sessions/"+id, nil)
			caps, _ := info["caps"].(map[string]any)
			if got, _ := caps[name].(bool); got != on {
				t.Errorf("cap %s=%v: GET /api/sessions/{sid} reports caps.%s=%v", name, on, name, caps[name])
			}
			if on {
				continue
			}
			switch name {
			case "ProcessList":
				out := e.ok("GET", "/api/sessions/"+id+"/processes", nil)
				if out["supported"] != false {
					t.Errorf("ProcessList off: GET /processes did not answer supported:false: %v", out)
				}
			case "Users":
				if out := e.ok("GET", "/api/sessions/"+id+"/users", nil); out["supported"] != false {
					t.Errorf("Users off: GET /users did not answer supported:false: %v", out)
				}
				if code, _ := e.call("POST", "/api/sessions/"+id+"/users/plan", map[string]any{"Kind": "create", "User": map[string]any{"Name": "amy"}}, "tok"); code < 400 {
					t.Errorf("Users off: POST /users/plan answered %d", code)
				}
			case "ReorderColumns":
				ref := map[string]any{"Schema": "public", "Name": "t1", "Kind": "table"}
				if code, _ := e.call("POST", "/api/sessions/"+id+"/plan/table", map[string]any{"ref": ref, "op": "move_column", "column": "name", "to": 0}, "tok"); code < 400 {
					t.Errorf("ReorderColumns off: move_column answered %d", code)
				}
			}
		}
	}

	// the client reads every capability: caps.<Name> in the TSX, or cap:<Name> in a shared menu list it filters with
	tsx := readTree(t, "web/src", ".ts", ".tsx")
	menuCaps := map[string]bool{}
	for _, list := range menus.All() {
		for _, it := range list {
			for _, p := range strings.Split(it.When, ";") {
				if strings.HasPrefix(p, "cap:") {
					menuCaps[strings.TrimPrefix(p, "cap:")] = true
				}
			}
		}
	}
	for _, name := range gatedCaps {
		if !regexp.MustCompile(`caps\??\.`+name+`\b`).MatchString(tsx) && !menuCaps[name] {
			t.Errorf("web/src never reads caps.%s and no shared menu has cap:%s: the web does not gate on it", name, name)
		}
	}
}

// TestParityBootstrapFailureReported: the terminal shows "Bootstrap command failed"; the web returns the failures from POST /sessions.
func TestParityBootstrapFailureReported(t *testing.T) {
	e := newEnv(t, "")
	e.s.o.Conns[0].Bootstrap = []string{"select 1", "boom"}
	e.fake.capsFn = nil
	out := e.ok("POST", "/api/sessions", map[string]any{"name": "c1"})
	_ = out["id"]
	if l, _ := out["bootstrapErrors"].([]any); len(l) != 1 || !strings.Contains(l[0].(string), "boom") {
		t.Errorf("bootstrapErrors = %v", out["bootstrapErrors"])
	}
}
