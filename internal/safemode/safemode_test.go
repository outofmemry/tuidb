package safemode

import "testing"

func TestGate(t *testing.T) {
	for _, c := range []struct {
		l                 Level
		readOnly          bool
		confirm, password bool
	}{
		{Silent, false, false, false},
		{Alert1, true, true, false},
		{Alert2, true, false, false},
		{Alert2, false, true, false},
		{Safe1, true, true, true},
		{Safe2, true, false, false},
		{Safe2, false, true, true},
	} {
		gc, gp := c.l.Gate(c.readOnly)
		if gc != c.confirm || gp != c.password {
			t.Errorf("%s readOnly=%v: got confirm=%v password=%v", c.l, c.readOnly, gc, gp)
		}
	}
	if _, err := Parse("bogus"); err == nil {
		t.Error("bogus must fail")
	}
	if l, _ := Parse(""); l != Silent {
		t.Error("empty is silent")
	}
}
