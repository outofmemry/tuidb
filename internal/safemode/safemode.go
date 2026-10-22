// Package safemode decides whether a statement batch needs confirmation or a password.
package safemode

import "fmt"

type Level string

const (
	Silent Level = "silent"
	Alert1 Level = "alert1"
	Alert2 Level = "alert2"
	Safe1  Level = "safe1"
	Safe2  Level = "safe2"
)

var All = []Level{Silent, Alert1, Alert2, Safe1, Safe2}

func Parse(s string) (Level, error) {
	if s == "" {
		return Silent, nil
	}
	for _, l := range All {
		if string(l) == s {
			return l, nil
		}
	}
	return Silent, fmt.Errorf("unknown safe mode %q (want silent|alert1|alert2|safe1|safe2)", s)
}

func (l Level) Label() string {
	return map[Level]string{Silent: "Silent Mode", Alert1: "Alert Mode 1", Alert2: "Alert Mode 2", Safe1: "Safe Mode 1", Safe2: "Safe Mode 2"}[l]
}

// Gate says what the user must do before the batch is sent.
// readOnly means every statement is SELECT/EXPLAIN/SHOW-like (see db.IsReadOnly).
func (l Level) Gate(readOnly bool) (confirm, password bool) {
	switch l {
	case Alert1:
		return true, false
	case Alert2:
		return !readOnly, false
	case Safe1:
		return true, true
	case Safe2:
		return !readOnly, !readOnly
	}
	return false, false
}
