package singlestore

import (
	"testing"

	"webtuidb/internal/db"
)

func TestAdjust(t *testing.T) {
	c := db.Capabilities{Triggers: true, ForeignKeys: true, Indexes: true, Dialect: "mysql"}
	adjust(&c)
	if c.Triggers || c.ForeignKeys || !c.Indexes || c.ExplainMode() != "text" {
		t.Errorf("%+v", c)
	}
}
