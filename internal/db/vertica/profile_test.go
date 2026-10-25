package vertica

import (
	"testing"

	"webtuidb/internal/db"
)

func TestAdjust(t *testing.T) {
	c := db.Capabilities{Triggers: true, MatViews: true, Transactions: true, Users: true, ProcessList: true, Dialect: "postgres"}
	adjust(&c)
	if c.Triggers || c.Transactions || c.Users || c.ProcessList || c.Dialect != "postgres" || c.ExplainMode() != "text" {
		t.Errorf("%+v", c)
	}
	if d, err := db.Lookup("vertica"); err != nil || d.Name() != "vertica" {
		t.Errorf("%v", err)
	}
}
