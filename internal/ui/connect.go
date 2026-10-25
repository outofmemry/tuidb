package ui

import (
	"context"
	"fmt"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// Opened is a live connection plus whatever must be closed with it.
type Opened struct {
	Sess     db.Session
	Password string // resolved password, used by Safe Mode 1/2 to re-authenticate
	closer   func()
}

func (o *Opened) Close() {
	if o.Sess != nil {
		o.Sess.Close()
	}
	if o.closer != nil {
		o.closer()
	}
}

// ConnectFunc opens a connection. Tests replace it with a fake.
type ConnectFunc func(ctx context.Context, c config.Connection) (*Opened, error)

// DefaultConnect resolves password_cmd, opens the SSH tunnel when configured, then the driver.
func DefaultConnect(ctx context.Context, c config.Connection) (*Opened, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pw, err := c.ResolvePassword(ctx)
	if err != nil {
		return nil, err
	}
	c.Password = pw
	drv, err := db.Lookup(c.Driver)
	if err != nil {
		return nil, err
	}
	o := &Opened{Password: pw}
	var dial db.DialFunc
	if c.SSH != nil && c.SSH.Host != "" {
		t, err := db.OpenTunnel(ctx, c.SSH)
		if err != nil {
			return nil, err
		}
		o.closer = func() { t.Close() }
		dial = t.Dial
	}
	if o.Sess, err = drv.Open(ctx, c, dial); err != nil {
		o.Close()
		return nil, fmt.Errorf("connect %s: %w", c.Name, err)
	}
	return o, nil
}
