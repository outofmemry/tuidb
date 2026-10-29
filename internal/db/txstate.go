package db

import "context"

// SchemaSetter is implemented by sessions whose unqualified names resolve through a current schema that can be
// changed (PostgreSQL's search_path). Both UIs call it when the sidebar switches schema, so the SQL editor,
// search in database, completion and the schema diff work in the schema on screen.
type SchemaSetter interface {
	SetSchema(ctx context.Context, name string) error
}

// TxReporter is implemented by pinned sessions that can ask the server whether a transaction is open.
type TxReporter interface {
	InTx() bool
}

// TxVerb tells what a statement does to the transaction: +1 starts one (BEGIN, START TRANSACTION), -1 ends it
// (COMMIT, ROLLBACK, END, ABORT; not ROLLBACK TO a savepoint), 0 anything else. A PL/SQL or T-SQL BEGIN … END
// block is not a transaction start: BEGIN must stand alone or be followed by TRANSACTION / WORK / TRAN / ISOLATION.
func TxVerb(dialect, stmt string) int {
	w := upper(scan(dialect, stmt))
	if len(w) == 0 {
		return 0
	}
	next := ""
	if len(w) > 1 {
		next = w[1]
	}
	switch w[0] {
	case "BEGIN":
		switch next {
		case "", ";", "TRANSACTION", "WORK", "TRAN", "ISOLATION", "READ", "DEFERRABLE", "NOT":
			return 1
		}
	case "START":
		if next == "TRANSACTION" {
			return 1
		}
	case "COMMIT", "END", "ABORT":
		if next == "" || next == ";" || next == "TRANSACTION" || next == "WORK" || next == "TRAN" || next == "AND" {
			return -1
		}
	case "ROLLBACK":
		for _, x := range w[1:] {
			if x == "TO" { // ROLLBACK TO SAVEPOINT keeps the transaction open
				return 0
			}
		}
		return -1
	}
	return 0
}

// TxAfter is whether a transaction is open after stmts ran, starting from open (a client-side guess for drivers
// that are not TxReporters).
func TxAfter(dialect string, open bool, stmts []string) bool {
	for _, s := range stmts {
		switch TxVerb(dialect, s) {
		case 1:
			open = true
		case -1:
			open = false
		}
	}
	return open
}

// InTx is whether sess holds an open transaction: the server's answer when sess can give one, else guess.
func InTx(sess Session, guess bool) bool {
	if r, ok := sess.(TxReporter); ok {
		return r.InTx()
	}
	return guess
}
