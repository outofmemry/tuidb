package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"webtuidb/internal/config"
	"webtuidb/internal/db"

	tea "charm.land/bubbletea/v2"
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

// boot runs the connection's bootstrap statements, then next(). A failing statement is shown, not fatal.
func (w *Workspace) boot(next func() tea.Cmd) tea.Cmd {
	stmts, sess := w.conn.Bootstrap, w.sess
	if len(stmts) == 0 {
		return next()
	}
	return liveCmd(func() func() tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		type done struct {
			sql string
			dur time.Duration
			err error
		}
		var out []done
		for _, s := range stmts {
			start := time.Now()
			_, err := sess.Exec(ctx, s, 1)
			out = append(out, done{s, time.Since(start), err})
		}
		return func() tea.Cmd {
			var bad []string
			for _, d := range out {
				w.log(d.sql, d.dur, d.err)
				if d.err != nil {
					bad = append(bad, d.sql+"\n  "+d.err.Error())
				}
			}
			if len(bad) > 0 {
				w.a.errorDialog("Bootstrap command failed (the connection is still open)", fmt.Errorf("%s", strings.Join(bad, "\n")))
			}
			return next()
		}
	})
}
