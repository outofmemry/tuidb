package redshift

import (
	"testing"

	"webtuidb/internal/db"
)

func TestAdjust(t *testing.T) {
	c := db.Capabilities{Triggers: true, MatViews: true, Indexes: true, ProcessList: true, Dialect: "postgres"}
	adjust(&c)
	if c.Triggers || c.MatViews || c.Indexes || c.ProcessList || c.Dialect != "postgres" || c.ExplainMode() != "text" {
		t.Errorf("%+v", c)
	}
	if d, err := db.Lookup("redshift"); err != nil || d.Name() != "redshift" {
		t.Errorf("%v", err)
	}
}
