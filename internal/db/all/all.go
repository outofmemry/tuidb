// Package all registers the capability-profile drivers; main blank-imports it next to the real ones.
package all

import (
	_ "webtuidb/internal/db/greenplum"
	_ "webtuidb/internal/db/redshift"
	_ "webtuidb/internal/db/singlestore"
	_ "webtuidb/internal/db/vertica"
)
