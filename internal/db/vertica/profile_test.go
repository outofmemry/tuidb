package vertica

import (
	"context"
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestAdjust(t *testing.T) {
	c := db.Capabilities{Schemas: true, Views: true, Triggers: true, MatViews: true, Transactions: true, Users: true, ProcessList: true, Dialect: "postgres"}
	adjust(&c)
	if c.Schemas || c.Views || c.Triggers || c.Transactions || c.Users || c.ProcessList || c.Dialect != "postgres" || c.ExplainMode() != "text" {
		t.Errorf("%+v", c)
	}
	if d, err := db.Lookup("vertica"); err != nil || d.Name() != "vertica" {
		t.Errorf("%v", err)
	}
}

func TestCatalogRefused(t *testing.T) {
	s := session{}
	if _, err := s.Objects(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "Vertica catalog browsing is not implemented; use the SQL editor") {
		t.Fatalf("%v", err)
	}
	if _, err := s.DDL(context.Background(), db.ObjRef{}); err == nil {
		t.Fatal("DDL must be refused")
	}
}
