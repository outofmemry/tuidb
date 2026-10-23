// Package vertica is a capability profile of the postgres driver: same protocol and dialect, its own
// name and the capabilities it really has.
package vertica

import (
	"context"
	"errors"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/db/postgres"
)

func init() { db.Register(driver{postgres.New(config.Vertica, adjust)}) }

// ErrCatalog is what every catalog-browsing call returns: those queries read pg_catalog, which Vertica does not have.
var ErrCatalog = errors.New("Vertica catalog browsing is not implemented; use the SQL editor")

// driver wraps the PostgreSQL driver so sessions refuse the pg_catalog browsing queries with a clear error.
type driver struct{ db.Driver }

func (d driver) Open(ctx context.Context, c config.Connection, dial db.DialFunc) (db.Session, error) {
	s, err := d.Driver.Open(ctx, c, dial)
	if err != nil {
		return nil, err
	}
	return session{s}, nil
}

type session struct{ db.Session }

func (session) Objects(context.Context, string) ([]db.Object, error)   { return nil, ErrCatalog }
func (session) Schemas(context.Context) ([]string, error)              { return nil, nil }
func (session) DDL(context.Context, db.ObjRef) (string, error)         { return "", ErrCatalog }
func (session) Indexes(context.Context, db.ObjRef) ([]db.Index, error) { return nil, nil }

func (session) Columns(context.Context, db.ObjRef) ([]db.Column, error) { return nil, ErrCatalog }
func (session) Databases(context.Context) ([]string, error)             { return nil, ErrCatalog }
func (s session) PlanTable(old, new *db.TableDef) ([]db.Statement, error) {
	if old == nil { // CREATE is built from the TableDef alone; only ALTER reads pg_catalog
		return s.Session.PlanTable(nil, new)
	}
	return nil, ErrCatalog
}

// Script refuses CREATE, which the wrapped driver builds from pg_catalog; DROP/TRUNCATE/SELECT scripts read no catalog.
func (s session) Script(ctx context.Context, kind db.ScriptKind, ref db.ObjRef) (string, error) {
	if kind == db.ScriptCreate {
		return "", ErrCatalog
	}
	return s.Session.Script(ctx, kind, ref)
}

func (session) Triggers(context.Context, db.ObjRef) ([]db.Trigger, error) { return nil, nil }

func (session) ForeignKeys(context.Context, db.ObjRef) ([]db.ForeignKey, error) { return nil, nil }

// Pin keeps the optional db.Pinner working through the wrapper.
func (s session) Pin(ctx context.Context) (db.Session, error) {
	p, ok := s.Session.(db.Pinner)
	if !ok {
		return nil, errors.New("this connection cannot be pinned")
	}
	ps, err := p.Pin(ctx)
	if err != nil {
		return nil, err
	}
	return session{ps}, nil
}

// Vertica has no BEGIN and its catalogs are v_catalog/v_monitor, not pg_catalog: advertise only what does not
// read pg_* views, and the session wrapper refuses Objects/DDL. shortcut: port the catalog queries to v_catalog
// before offering more.
func adjust(c *db.Capabilities) {
	c.Schemas, c.Views = false, false
	c.Triggers, c.MatViews, c.Indexes, c.ForeignKeys, c.Functions, c.Sequences = false, false, false, false, false, false
	c.RenameDB, c.Transactions, c.ProcessList, c.Users, c.Explain = false, false, false, false, "text"
}
