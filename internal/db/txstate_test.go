package db

import "testing"

func TestTxVerb(t *testing.T) {
	for stmt, want := range map[string]int{
		"begin;": 1, "BEGIN": 1, "  begin transaction isolation level serializable": 1, "start transaction read only": 1,
		"BEGIN WORK": 1, "-- note\nbegin": 1, "begin tran": 1,
		"commit": -1, "COMMIT;": -1, "end": -1, "rollback": -1, "ROLLBACK WORK": -1, "abort": -1, "commit and chain": -1,
		"rollback to savepoint a": 0, "begin\n  select 1;\nend": 0, "select 'begin'": 0, "update t set a = 1": 0, "": 0,
		"start replica": 0,
	} {
		if got := TxVerb("postgres", stmt); got != want {
			t.Errorf("TxVerb(%q) = %d, want %d", stmt, got, want)
		}
	}
	if !TxAfter("postgres", false, []string{"begin", "update t set a=1"}) || TxAfter("postgres", true, []string{"select 1", "rollback"}) {
		t.Error("TxAfter")
	}
}
