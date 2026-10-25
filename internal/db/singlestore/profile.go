// Package singlestore is a capability profile of the mysql driver: same protocol and dialect, its own name
// and the capabilities it really has.
package singlestore

import (
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/mysql"
)

func init() { db.Register(mysql.New(config.SingleStore, adjust)) }

// SingleStore documents no triggers and no (enforced) foreign keys; its EXPLAIN is plain text.
func adjust(c *db.Capabilities) { c.Triggers, c.ForeignKeys, c.Explain = false, false, "text" }
