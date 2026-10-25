package cassandra

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"webtuidb/internal/db"
)

// Split and IsReadOnly must read a statement the way the server does, or a write can hide in what looks like a string or a comment.
// Each case is something the server accepts (or rejects) because of one lexical rule; Split must cut it the same way.
func TestIntegrationLexingAgreesWithServer(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, c := range []struct {
		stmt       string
		serverOK   bool
		statements int // what Split makes of it
	}{
		{"SELECT key FROM system.local -- ; DROP KEYSPACE x", true, 1},
		{"SELECT key FROM system.local // ; DROP KEYSPACE x", true, 1},
		{"SELECT key FROM system.local /* ; DROP KEYSPACE x */", true, 1},
		{"SELECT key /* a /* b */ FROM system.local", true, 1}, // block comments do not nest
		{"SELECT key FROM system.local WHERE key = 'it''s;'", true, 1},
		{`SELECT key FROM system.local WHERE key = '\'`, true, 1}, // a backslash is an ordinary character
		{"SELECT key FROM system.local WHERE key = $$a;'b$$", true, 1},
		{`SELECT "key" FROM system."local"`, true, 1},
		{`SELECT key AS "a;b" FROM system.local`, true, 1},
		{"SELECT key FROM system.local # x", false, 1}, // # is no comment
		{"SELECT key FROM system.local; SELECT 1", false, 2},
		{"SELECT key FROM system.local /* a /* b */ */", false, 1},
		{"SELECT key FROM system.local -- a\n; DROP KEYSPACE nope_nope", false, 2},
	} {
		_, err := s.Exec(ctx, c.stmt, 0)
		if (err == nil) != c.serverOK && c.statements == 1 {
			t.Errorf("%q: server ok=%v err=%v", c.stmt, c.serverOK, err)
		}
		if n := len(db.Split("cql", c.stmt)); n != c.statements {
			t.Errorf("%q: Split made %d statements, want %d", c.stmt, n, c.statements)
		}
	}
	// the dangerous one: the server runs the DELETE, so Split must hand it over as its own, writing, statement
	text := `SELECT '\'; DELETE FROM customers WHERE id = 99999; --'`
	st := db.Split("cql", text)
	if len(st) != 2 || !db.IsReadOnly("cql", st[0].Text) || db.IsReadOnly("cql", st[1].Text) {
		t.Fatalf("hidden write: %+v", st)
	}
	// a batch reaches the server whole, and is not read-only
	b := db.Split("cql", "BEGIN BATCH DELETE FROM customers WHERE id = 99998; DELETE FROM customers WHERE id = 99999; APPLY BATCH; SELECT 1 FROM system.local")
	if len(b) != 2 || db.IsReadOnly("cql", b[0].Text) || !db.IsReadOnly("cql", b[1].Text) {
		t.Fatalf("batch split: %+v", b)
	}
	if _, err := s.Exec(ctx, b[0].Text, 0); err != nil {
		t.Errorf("batch: %v", err)
	}
}

func TestIntegrationDialFunc(t *testing.T) {
	seed(t)
	c := testConn(t)
	var dials atomic.Int32
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
	}
	s, err := (drv{}).Open(context.Background(), c, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Exec(context.Background(), "SELECT * FROM customers", 0); err != nil {
		t.Fatal(err)
	}
	if dials.Load() == 0 {
		t.Error("the tunnel's DialFunc was not used")
	}
}

func TestIntegrationTLSIsAttempted(t *testing.T) {
	seed(t)
	c := testConn(t)
	c.SSLMode = "require"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if s, err := (drv{}).Open(ctx, c, nil); err == nil { // the docker server speaks plaintext only
		s.Close()
		t.Error("ssl_mode=require must not silently fall back to plaintext")
	}
}
