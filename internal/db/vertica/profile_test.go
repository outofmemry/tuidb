package vertica

import (
	"context"
	"errors"
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

func TestCatalogRefusedThroughDriver(t *testing.T) {
	d, err := db.Lookup("vertica")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(driver); !ok {
		t.Fatalf("registered driver is %T, not the wrapper", d)
	}
	ctx := context.Background()
	var s db.Session = session{}
	checks := map[string]error{}
	_, checks["Columns"] = s.Columns(ctx, db.ObjRef{})
	_, checks["Databases"] = s.Databases(ctx)
	_, checks["PlanTable"] = s.PlanTable(&db.TableDef{}, &db.TableDef{})
	_, checks["Script"] = s.Script(ctx, db.ScriptCreate, db.ObjRef{})
	for name, err := range checks {
		if !errors.Is(err, ErrCatalog) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

type planStub struct{ db.Session }

func (planStub) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	return []db.Statement{{Text: "CREATE TABLE x ()"}}, nil
}

func TestCreateTableNeedsNoCatalog(t *testing.T) {
	st, err := session{planStub{}}.PlanTable(nil, &db.TableDef{})
	if err != nil || len(st) != 1 {
		t.Errorf("CREATE TABLE must reach the wrapped planner: %v %v", st, err)
	}
}
