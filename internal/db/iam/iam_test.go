package iam

import (
	"context"
	"strings"
	"testing"

	"webtuidb/internal/config"
)

func TestTokenRejectsNonRDSDrivers(t *testing.T) {
	for _, d := range []string{config.Redshift, config.Greenplum, config.Vertica, config.SingleStore} {
		_, err := Token(context.Background(), config.Connection{Driver: d, Host: "h", User: "u"})
		if err == nil || !strings.Contains(err.Error(), "not available for "+d) {
			t.Errorf("%s: %v", d, err)
		}
	}
}
