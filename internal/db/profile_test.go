package db_test

import (
	"testing"

	"webtuidb/internal/db"
	_ "webtuidb/internal/db/all"
)

// Opening needs a server, so check what the profiles register: name, and the Caps a session
// would start from are exercised by the drivers' own integration tests.
func TestProfileDriversRegistered(t *testing.T) {
	for _, n := range []string{"redshift", "greenplum", "vertica", "singlestore"} {
		d, err := db.Lookup(n)
		if err != nil || d.Name() != n {
			t.Errorf("%s: %v", n, err)
		}
	}
}
