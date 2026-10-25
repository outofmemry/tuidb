package ui

import (
	"testing"

	_ "webtuidb/internal/db/mariadb"
)

func TestE2EMariaDB(t *testing.T) {
	e2eSQL(t, "mariadb", "WEBTUIDB_TEST_URL_MARIADB", "mariadb://webtuidb:webtuidb@127.0.0.1:53307/webtuidb")
}
