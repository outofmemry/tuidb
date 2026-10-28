package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/db"
)

// M13.3 MySQL per-account resource limits.

var mysqlLimitNames = []string{"MAX_QUERIES_PER_HOUR", "MAX_UPDATES_PER_HOUR", "MAX_CONNECTIONS_PER_HOUR", "MAX_USER_CONNECTIONS"}

// userLimits is the four-number form; 0 means unlimited (the server default for MAX_USER_CONNECTIONS). It plans one UserChange{Kind: "limits"}.
func (w *Workspace) userLimits(ua db.UserAdmin, u db.User) tea.Cmd {
	if w.isPG() {
		return w.pgConnLimit(ua, u)
	}
	if u.Limits == nil { // the max_* columns could not be read: showing zeros and saving them would clear the quotas
		return w.a.say("resource limits could not be read for %s", userLabel(u))
	}
	var fs []formField
	for _, n := range mysqlLimitNames {
		hint := "0 = unlimited"
		if n == "MAX_USER_CONNECTIONS" {
			hint = "0 = the server default" // not "no limit": the global max_user_connections applies
		}
		fs = append(fs, formField{Key: n, Label: strings.ToLower(strings.ReplaceAll(n, "_", " ")), Kind: fText, Value: strconv.Itoa(u.Limits[n]), Hint: hint})
	}
	f := newForm("Resource limits of "+userLabel(u), fs, func(v map[string]string) tea.Cmd {
		lim := map[string]int{}
		for _, n := range mysqlLimitNames {
			x, _ := strconv.Atoi(strings.TrimSpace(v[n]))
			lim[n] = x
		}
		return w.userApply(ua, "limits changed for "+userLabel(u), db.UserChange{Kind: "limits", User: db.User{Name: u.Name, Host: u.Host, Limits: lim}})
	})
	f.validate = func(v map[string]string) error {
		for _, n := range mysqlLimitNames {
			if x, err := strconv.Atoi(strings.TrimSpace(v[n])); err != nil || x < 0 {
				return fmt.Errorf("%s must be a whole number, 0 or more", n)
			}
		}
		return nil
	}
	w.a.push(f)
	return nil
}

// pgConnLimit is the Postgres role's CONNECTION LIMIT (-1 = no limit); it plans one UserChange{Kind: "limits"}.
func (w *Workspace) pgConnLimit(ua db.UserAdmin, u db.User) tea.Cmd {
	cur, ok := u.Limits["CONNECTION_LIMIT"]
	if !ok {
		cur = -1
	}
	f := newForm("Connection limit of "+userLabel(u), []formField{{Key: "n", Label: "connection limit", Kind: fText, Value: strconv.Itoa(cur), Hint: "-1 = no limit"}},
		func(v map[string]string) tea.Cmd {
			n, _ := strconv.Atoi(strings.TrimSpace(v["n"]))
			return w.userApply(ua, "connection limit changed for "+userLabel(u), db.UserChange{Kind: "limits", User: db.User{Name: u.Name, Host: u.Host, Limits: map[string]int{"CONNECTION_LIMIT": n}}})
		})
	f.validate = func(v map[string]string) error {
		if x, err := strconv.Atoi(strings.TrimSpace(v["n"])); err != nil || x < -1 {
			return fmt.Errorf("the connection limit must be a whole number, -1 or more")
		}
		return nil
	}
	w.a.push(f)
	return nil
}
