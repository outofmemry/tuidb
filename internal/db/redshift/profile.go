// Package redshift is a capability profile of the postgres driver: same protocol and dialect, its own
// name and the capabilities it really has.
package redshift

import (
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/postgres"
)

func init() { db.Register(postgres.New(config.Redshift, adjust)) }

// No indexes, no pg_stat_activity, and EXPLAIN is plain text.
func adjust(c *db.Capabilities) {
	c.Triggers, c.MatViews, c.Indexes, c.ProcessList, c.Explain = false, false, false, false, "text"
}
