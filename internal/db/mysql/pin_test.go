package mysql

import (
	"context"
	"testing"

	"webtuidb/internal/db"
)

func TestPinKeepsTransaction(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	tbl := tempTable(t, s)
	exec(t, s, "CREATE TABLE "+q(tbl)+" (a int) ENGINE=InnoDB")
	pin, err := s.Pin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	for _, st := range []string{"BEGIN", "INSERT INTO " + q(tbl) + " VALUES (1)"} {
		if _, err := pin.Exec(ctx, st, 0); err != nil {
			t.Fatal(err)
		}
	}
	if r := exec(t, s, "SELECT a FROM "+q(tbl)); len(r.Rows) != 0 {
		t.Errorf("uncommitted row visible outside the pin: %v", r.Rows)
	}
	if _, err := pin.Exec(ctx, "ROLLBACK", 0); err != nil {
		t.Fatal(err)
	}
	if r, err := pin.Exec(ctx, "SELECT a FROM "+q(tbl), 0); err != nil || len(r.Rows) != 0 {
		t.Errorf("after rollback: %v %v", r, err)
	}
	var _ db.Session = pin
}
