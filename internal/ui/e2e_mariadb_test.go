package ui

import (
	"testing"

	_ "tuidb/internal/db/mariadb"
)

func TestE2EMariaDB(t *testing.T) {
	e2eSQL(t, "mariadb", "TUIDB_TEST_URL_MARIADB", "mariadb://tuidb:tuidb@127.0.0.1:53307/tuidb")
}
