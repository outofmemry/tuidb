package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

func TestApproverGatesUnsafeStatements(t *testing.T) {
	s := New(config.Config{MCP: config.MCP{ApprovalForUnsafe: true}, General: config.General{RowLimit: 10}}, []config.Connection{{Name: "c", Driver: "fake"}})
	s.connect = func(context.Context, config.Connection) (db.Session, func(), error) {
		return &fakeSess{}, func() {}, nil
	}
	full := &config.MCPToken{Name: "f", Scope: "full"}
	call(t, s, full, "open_connection", map[string]any{"connection": "c"})
	args := map[string]any{"connection": "c", "sql": "UPDATE t SET a = 1"}

	var asked Approval
	s.Approver = func(_ context.Context, a Approval) (bool, error) { asked = a; return false, nil }
	if out, bad := call(t, s, full, "execute_query", args); !bad || !strings.Contains(out, "denied") || asked.Connection != "c" {
		t.Fatalf("denied answer should refuse: bad=%v out=%s asked=%+v", bad, out, asked)
	}
	s.Approver = func(context.Context, Approval) (bool, error) { return true, nil }
	if out, bad := call(t, s, full, "execute_query", args); bad {
		t.Fatalf("approved statement should run: %s", out)
	}
	s.Approver = func(context.Context, Approval) (bool, error) { return false, errors.New("sheet closed") }
	if out, bad := call(t, s, full, "execute_query", args); !bad || !strings.Contains(out, "sheet closed") {
		t.Fatalf("approver error should propagate: %s", out)
	}
}
