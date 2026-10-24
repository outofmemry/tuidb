// Package greenplum is a capability profile of the postgres driver: same protocol and dialect, its own name
// and the capabilities it really has.
package greenplum

import (
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/postgres"
)

func init() { db.Register(postgres.New(config.Greenplum, nil)) }
