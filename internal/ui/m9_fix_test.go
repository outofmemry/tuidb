package ui

import "testing"

func TestClipboardCSVNeutralizesFormulas(t *testing.T) {
	if got := csvLine([]string{"=1+1", "ok", "@x"}); got != "'=1+1,ok,'@x\n" {
		t.Fatalf("csvLine = %q", got)
	}
}
