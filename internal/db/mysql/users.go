package mysql

import (
	"context"
	"fmt"
	"strings"

	"webtuidb/internal/db"
)

// words are the privileges (static and MySQL 8 dynamic) that cannot be quoted, so only these are allowed.
var words = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Split(`ALL|ALL PRIVILEGES|ALTER|ALTER ROUTINE|CREATE|CREATE ROLE|CREATE ROUTINE|CREATE TABLESPACE|
		CREATE TEMPORARY TABLES|CREATE USER|CREATE VIEW|DELETE|DROP|DROP ROLE|EVENT|EXECUTE|FILE|GRANT OPTION|INDEX|INSERT|
		LOCK TABLES|PROCESS|REFERENCES|RELOAD|REPLICATION CLIENT|REPLICATION SLAVE|SELECT|SHOW DATABASES|SHOW VIEW|SHUTDOWN|
		SUPER|TRIGGER|UPDATE|USAGE|APPLICATION_PASSWORD_ADMIN|AUDIT_ABORT_EXEMPT|AUDIT_ADMIN|AUTHENTICATION_POLICY_ADMIN|
		BACKUP_ADMIN|BINLOG_ADMIN|BINLOG_ENCRYPTION_ADMIN|CLONE_ADMIN|CONNECTION_ADMIN|ENCRYPTION_KEY_ADMIN|FIREWALL_ADMIN|
		FIREWALL_EXEMPT|FIREWALL_USER|FLUSH_OPTIMIZER_COSTS|FLUSH_STATUS|FLUSH_TABLES|FLUSH_USER_RESOURCES|
		GROUP_REPLICATION_ADMIN|GROUP_REPLICATION_STREAM|INNODB_REDO_LOG_ARCHIVE|INNODB_REDO_LOG_ENABLE|NDB_STORED_USER|
		PASSWORDLESS_USER_ADMIN|PERSIST_RO_VARIABLES_ADMIN|REPLICATION_APPLIER|REPLICATION_SLAVE_ADMIN|RESOURCE_GROUP_ADMIN|
		RESOURCE_GROUP_USER|ROLE_ADMIN|SENSITIVE_VARIABLES_OBSERVER|SERVICE_CONNECTION_ADMIN|SESSION_VARIABLES_ADMIN|
		SET_ANY_DEFINER|SET_USER_ID|SHOW_ROUTINE|SKIP_QUERY_REWRITE|SYSTEM_USER|SYSTEM_VARIABLES_ADMIN|TABLE_ENCRYPTION_ADMIN|
		TELEMETRY_LOG_ADMIN|TRANSACTION_GTID_TAG|XA_RECOVER_ADMIN`, "|") {
		m[strings.Join(strings.Fields(w), " ")] = true
	}
	return m
}()

func keywords(in []string) (string, error) {
	out := make([]string, 0, len(in))
	for _, w := range in {
		w = strings.ToUpper(strings.TrimSpace(w))
		if !words[w] {
			return "", fmt.Errorf("bad privilege %q", w)
		}
		if w == "ALL" {
			w = "ALL PRIVILEGES"
		}
		out = append(out, w)
	}
	return strings.Join(out, ", "), nil
}

var mysqlLimits = []string{"MAX_QUERIES_PER_HOUR", "MAX_UPDATES_PER_HOUR", "MAX_CONNECTIONS_PER_HOUR", "MAX_USER_CONNECTIONS"}

func acct(name, host string) string {
	if host == "" {
		host = "%"
	}
	return db.MySQL.Literal(name) + "@" + db.MySQL.Literal(host)
}

// object turns "db.table" / "db.*" / "*.*" into quoted SQL.
func object(o string) string {
	d, t, _ := strings.Cut(o, ".")
	if t == "" {
		t = "*"
	}
	part := func(p string) string {
		if p == "*" {
			return p
		}
		return db.MySQL.Quote(p)
	}
	return part(d) + "." + part(t)
}

// shortcut: privileges come from parsing SHOW GRANTS (one query per account); roles are skipped. Upgrade to mysql.db / mysql.tables_priv if that gets slow.
func (s *session) Users(ctx context.Context) ([]db.User, error) {
	rows, err := s.db().QueryContext(ctx, "SELECT User, Host FROM mysql.user ORDER BY User, Host")
	if err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	var out []db.User
	for rows.Next() {
		var u db.User
		if err := rows.Scan(&u.Name, &u.Host); err != nil {
			rows.Close()
			return nil, fmt.Errorf("users: %w", err)
		}
		out = append(out, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.fillLimits(ctx, out)
	for i := range out {
		gr, err := s.db().QueryContext(ctx, "SHOW GRANTS FOR "+acct(out[i].Name, out[i].Host))
		if err != nil {
			continue // e.g. an account the current user may not inspect
		}
		for gr.Next() {
			var line string
			if gr.Scan(&line) == nil {
				addGrant(&out[i], line)
			}
		}
		gr.Close()
	}
	return out, nil
}

// addGrant parses "GRANT SELECT, INSERT ON `db`.* TO ..." into u.
func addGrant(u *db.User, line string) {
	rest, ok := strings.CutPrefix(line, "GRANT ")
	if !ok {
		return
	}
	privs, rest, ok := strings.Cut(rest, " ON ")
	if !ok {
		return // a role grant
	}
	obj, _, _ := strings.Cut(rest, " TO ")
	var ps []string
	var colGrants []db.Grant
	obj = strings.ReplaceAll(obj, "`", "")
	for _, p := range splitPrivs(privs) {
		if p == "ALL PRIVILEGES" {
			p = "ALL"
		}
		if name, cols, ok := strings.Cut(p, " ("); ok { // SELECT (a, b)
			var cs []string
			for _, c := range strings.Split(strings.TrimSuffix(cols, ")"), ",") {
				cs = append(cs, strings.Trim(strings.TrimSpace(c), "`"))
			}
			colGrants = append(colGrants, db.Grant{Object: obj, Privs: []string{name}, Columns: cs})
		} else if p != "USAGE" {
			ps = append(ps, p)
		}
	}
	if obj == "*.*" {
		u.Attrs = append(u.Attrs, ps...)
		return
	}
	if len(ps) > 0 {
		u.Grants = append(u.Grants, db.Grant{Object: obj, Privs: ps})
	}
	u.Grants = append(u.Grants, colGrants...)
}

// splitPrivs splits "SELECT (a, b), UPDATE" on the commas outside parentheses.
func splitPrivs(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

func (s *session) PlanUser(c db.UserChange) ([]db.Statement, error) {
	if c.User.Name == "" {
		return nil, fmt.Errorf("user needs a name")
	}
	// db.MySQL.Literal doubles backslashes, which is wrong under NO_BACKSLASH_ESCAPES, so refuse them.
	if strings.Contains(c.User.Name+c.User.Host+c.Password, `\`) {
		return nil, fmt.Errorf("backslash is not allowed in names or passwords")
	}
	if c.Password == "" && (c.Kind == "create" || c.Kind == "password") {
		return nil, fmt.Errorf("password required")
	}
	who := acct(c.User.Name, c.User.Host)
	one := func(f func(pw string) string) []db.Statement {
		return []db.Statement{{Text: f(db.MySQL.Literal(c.Password)), Display: f("'********'")}}
	}
	switch c.Kind {
	case "create":
		return one(func(pw string) string { return "CREATE USER " + who + " IDENTIFIED BY " + pw }), nil
	case "password":
		return one(func(pw string) string { return "ALTER USER " + who + " IDENTIFIED BY " + pw }), nil
	case "drop":
		return []db.Statement{{Text: "DROP USER " + who}}, nil
	case "limits":
		var parts []string
		for _, k := range mysqlLimits { // fixed order and a whitelist: the key is spliced into SQL
			if n, ok := c.User.Limits[k]; ok {
				if n < 0 {
					return nil, fmt.Errorf("%s must be 0 or more", k)
				}
				parts = append(parts, fmt.Sprintf("%s %d", k, n))
			}
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("limits needs at least one of %s", strings.Join(mysqlLimits, ", "))
		}
		return []db.Statement{{Text: "ALTER USER " + who + " WITH " + strings.Join(parts, " ")}}, nil
	case "attrs":
		privs, err := keywords(c.User.Attrs)
		if err != nil || privs == "" {
			return nil, fmt.Errorf("attrs needs privileges")
		}
		return []db.Statement{{Text: "GRANT " + privs + " ON *.* TO " + who}}, nil
	case "grant", "revoke":
		privs, err := keywords(c.Grant.Privs)
		if err != nil || privs == "" {
			return nil, fmt.Errorf("grant needs privileges")
		}
		if len(c.Grant.Columns) > 0 { // GRANT SELECT (a, b), UPDATE (a, b) ON ...
			qs := make([]string, len(c.Grant.Columns))
			for i, col := range c.Grant.Columns {
				qs[i] = db.MySQL.Quote(col)
			}
			cols := "(" + strings.Join(qs, ", ") + ")"
			privs = strings.ReplaceAll(privs, ", ", " "+cols+", ") + " " + cols
		}
		if c.Kind == "grant" {
			return []db.Statement{{Text: "GRANT " + privs + " ON " + object(c.Grant.Object) + " TO " + who}}, nil
		}
		return []db.Statement{{Text: "REVOKE " + privs + " ON " + object(c.Grant.Object) + " FROM " + who}}, nil
	}
	return nil, fmt.Errorf("unknown user change %q", c.Kind)
}

var _ db.UserAdmin = (*session)(nil)
