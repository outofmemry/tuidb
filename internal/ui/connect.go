package ui

import (
	"context"
	"errors"
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

// askSecret prompts for a connection password (password = "ask"); set by NewApp.
var askSecret func(c config.Connection) (string, error)

// ConnectFunc opens a connection. Tests replace it with a fake.
type ConnectFunc func(ctx context.Context, c config.Connection) (*Opened, error)

// DefaultConnect resolves password_cmd, opens the SSH tunnel when configured, then the driver.
func DefaultConnect(ctx context.Context, c config.Connection) (*Opened, error) {
	rctx, rcancel := context.WithTimeout(ctx, 30*time.Second) // password_cmd only
	pw, err := c.ResolvePassword(rctx)
	rcancel()
	if errors.Is(err, config.ErrPasswordAsk) && askSecret != nil {
		pw, err = askSecret(c) // before the connect timeout starts: typing a password must not eat the 30 s
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c.Password = pw
	drv, err := db.Lookup(c.Driver)
	if err != nil {
		return nil, err
	}
	o := &Opened{Password: pw}
	dial, closer, err := db.DialFor(ctx, c)
	if err != nil {
		return nil, err
	}
	o.closer = closer
	if o.Sess, err = drv.Open(ctx, c, dial); err != nil {
		o.Close()
		return nil, fmt.Errorf("connect %s: %w", c.Name, err)
	}
	return o, nil
}
