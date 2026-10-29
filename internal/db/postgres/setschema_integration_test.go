package postgres

import (
	"context"
	"testing"

	"webtuidb/internal/db"
)

func TestIntegrationSetSchemaAndTx(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	must(s.Exec(ctx, "DROP SCHEMA IF EXISTS wt_setschema CASCADE", 0))
	must(s.Exec(ctx, "CREATE SCHEMA wt_setschema", 0))
	t.Cleanup(func() { s.Exec(context.Background(), "DROP SCHEMA IF EXISTS wt_setschema CASCADE", 0) })
	must(s.Exec(ctx, "CREATE TABLE wt_setschema.only_here (id int)", 0))
	if _, err := s.Exec(ctx, "SELECT * FROM only_here", 0); err == nil {
		t.Fatal("only_here must not resolve in public")
	}
	if err := s.(db.SchemaSetter).SetSchema(ctx, "wt_setschema"); err != nil || s.CurrentSchema() != "wt_setschema" {
		t.Fatalf("SetSchema: %v %s", err, s.CurrentSchema())
	}
	for i := 0; i < 3; i++ { // every pooled connection, not just one
		if _, err := s.Exec(ctx, "SELECT * FROM only_here", 0); err != nil {
			t.Fatalf("after SetSchema: %v", err)
		}
	}
	p := must(s.(db.Pinner).Pin(ctx))
	defer p.Close()
	if _, err := p.Exec(ctx, "SELECT * FROM only_here", 0); err != nil {
		t.Fatalf("pinned after SetSchema: %v", err)
	}
	if db.InTx(p, false) {
		t.Error("no transaction yet")
	}
	must(p.Exec(ctx, "BEGIN", 0))
	must(p.Exec(ctx, "INSERT INTO only_here VALUES (1)", 0))
	if !db.InTx(p, false) {
		t.Error("BEGIN must show as an open transaction")
	}
	must(p.Exec(ctx, "ROLLBACK", 0))
	if db.InTx(p, true) {
		t.Error("ROLLBACK must end it")
	}
	if err := s.(db.SchemaSetter).SetSchema(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exec(ctx, "SELECT * FROM only_here", 0); err == nil {
		t.Error("back on public, only_here must not resolve")
	}
	if _, err := p.Exec(ctx, "SELECT * FROM only_here", 0); err == nil {
		t.Error("the pinned connection must follow the switch back")
	}
}
