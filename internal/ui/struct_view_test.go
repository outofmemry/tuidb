package ui

import (
	"strings"
	"testing"

	"webtuidb/internal/db"
)

func TestTypeChoicesDoublePrecisionIsOneEntry(t *testing.T) {
	for _, s := range typeChoices(db.Capabilities{}) {
		if strings.Contains(s, `\`) {
			t.Errorf("stray backslash in %q", s)
		}
	}
}
