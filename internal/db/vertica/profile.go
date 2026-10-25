// Package vertica is a capability profile of the postgres driver: same protocol and dialect, its own
// name and the capabilities it really has.
package vertica

import (
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/postgres"
)

func init() { db.Register(postgres.New(config.Vertica, adjust)) }

// Vertica has no BEGIN and its catalogs are v_catalog/v_monitor, not pg_catalog: advertise only what does not
// read pg_* views. shortcut: the sidebar, structure and DDL tabs still query pg_catalog; port them to
// v_catalog before offering more.
func adjust(c *db.Capabilities) {
	c.Triggers, c.MatViews, c.Indexes, c.ForeignKeys, c.Functions, c.Sequences = false, false, false, false, false, false
	c.RenameDB, c.Transactions, c.ProcessList, c.Users, c.Explain = false, false, false, false, "text"
}
