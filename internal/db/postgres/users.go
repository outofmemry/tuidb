package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"webtuidb/internal/db"
)

// words are bare SQL keywords (attributes, privileges) that cannot be quoted, so only these are allowed.
var words = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`SUPERUSER NOSUPERUSER CREATEDB NOCREATEDB CREATEROLE NOCREATEROLE INHERIT NOINHERIT
		LOGIN NOLOGIN REPLICATION NOREPLICATION BYPASSRLS NOBYPASSRLS
		SELECT INSERT UPDATE DELETE TRUNCATE REFERENCES TRIGGER USAGE CREATE CONNECT TEMPORARY EXECUTE ALL`) {
		m[w] = true
	}
	m["ALL PRIVILEGES"] = true
	return m
}()

// eLit is an E” string, which reads the same whether or not standard_conforming_strings is on.
func eLit(s string) string {
	return "E'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
}

func keywords(in []string, sep string) (string, error) {
	out := make([]string, 0, len(in))
	for _, w := range in {
		w = strings.ToUpper(strings.TrimSpace(w))
		if !words[w] {
			return "", fmt.Errorf("bad keyword %q", w)
		}
		out = append(out, w)
	}
	return strings.Join(out, sep), nil
}

// shortcut: sequence and schema privileges are not listed (tables, views and foreign tables only).
func (s *session) Users(ctx context.Context) ([]db.User, error) {
	var out []db.User
	idx := map[string]int{}
	err := s.query(ctx, `SELECT rolname::text, rolsuper, rolcreaterole, rolcreatedb, rolcanlogin, rolreplication, rolbypassrls, rolconnlimit
		FROM pg_roles WHERE rolname !~ '^pg_' ORDER BY 1`, nil, func(r pgx.Rows) error {
		var u db.User
		var f [6]bool
		var lim int32
		if err := r.Scan(&u.Name, &f[0], &f[1], &f[2], &f[3], &f[4], &f[5], &lim); err != nil {
			return err
		}
		if lim >= 0 {
			u.Limits = map[string]int{"CONNECTION_LIMIT": int(lim)}
		}
		for i, n := range []string{"SUPERUSER", "CREATEROLE", "CREATEDB", "LOGIN", "REPLICATION", "BYPASSRLS"} {
			if f[i] {
				u.Attrs = append(u.Attrs, n)
			}
		}
		idx[u.Name] = len(out)
		out = append(out, u)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Read the ACLs straight from pg_class/pg_attribute: the information_schema views replan a correlated
	// NOT EXISTS per row and take ~10 s on a stock cluster. System schemas are left out.
	err = s.query(ctx, `SELECT r.rolname::text, n.nspname || '.' || c.relname, string_agg(DISTINCT a.privilege_type, ',' ORDER BY a.privilege_type)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(coalesce(c.relacl, acldefault('r', c.relowner))) a
		JOIN pg_roles r ON r.oid = a.grantee
		WHERE c.relkind IN ('r','v','f','p') AND n.nspname <> 'information_schema' AND n.nspname !~ '^pg_'
		GROUP BY 1, 2 ORDER BY 1, 2`, nil, func(r pgx.Rows) error {
		var who, obj, privs string
		if err := r.Scan(&who, &obj, &privs); err != nil {
			return err
		}
		if i, ok := idx[who]; ok {
			out[i].Grants = append(out[i].Grants, db.Grant{Object: obj, Privs: strings.Split(privs, ",")})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Column grants only: attacl holds just the explicit column-level ACL entries.
	err = s.query(ctx, `SELECT r.rolname::text, n.nspname || '.' || c.relname, a.privilege_type, string_agg(att.attname::text, ',' ORDER BY att.attname)
		FROM pg_attribute att JOIN pg_class c ON c.oid = att.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL aclexplode(att.attacl) a
		JOIN pg_roles r ON r.oid = a.grantee
		WHERE att.attacl IS NOT NULL AND att.attnum > 0 AND NOT att.attisdropped
			AND c.relkind IN ('r','v','f','p') AND n.nspname <> 'information_schema' AND n.nspname !~ '^pg_'
		GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, nil, func(r pgx.Rows) error {
		var who, obj, priv, cols string
		if err := r.Scan(&who, &obj, &priv, &cols); err != nil {
			return err
		}
		if i, ok := idx[who]; ok {
			out[i].Grants = append(out[i].Grants, db.Grant{Object: obj, Privs: []string{priv}, Columns: strings.Split(cols, ",")})
		}
		return nil
	})
	return out, err
}

func (s *session) PlanUser(c db.UserChange) ([]db.Statement, error) {
	q := db.PG.Quote
	if c.User.Name == "" {
		return nil, fmt.Errorf("user needs a name")
	}
	role := q(c.User.Name)
	// build renders the statement with the password literal swapped, so Display never shows it.
	one := func(f func(pw string) string) []db.Statement {
		return []db.Statement{{Text: f(eLit(c.Password)), Display: f("'********'")}}
	}
	switch c.Kind {
	case "create", "attrs":
		attrs, err := keywords(c.User.Attrs, " ")
		if err != nil {
			return nil, err
		}
		verb := "CREATE ROLE "
		if c.Kind == "attrs" {
			verb = "ALTER ROLE "
		}
		return one(func(pw string) string {
			t := verb + role
			if attrs != "" {
				t += " WITH " + attrs
			}
			if c.Kind == "create" && c.Password != "" {
				t += " PASSWORD " + pw
			}
			return t
		}), nil
	case "limits":
		n, ok := c.User.Limits["CONNECTION_LIMIT"]
		if !ok || n < -1 {
			return nil, fmt.Errorf("limits needs CONNECTION_LIMIT (-1 for none)")
		}
		return []db.Statement{{Text: fmt.Sprintf("ALTER ROLE %s CONNECTION LIMIT %d", role, n)}}, nil
	case "drop":
		return []db.Statement{{Text: "DROP ROLE " + role}}, nil
	case "password":
		if c.Password == "" {
			return nil, fmt.Errorf("password required")
		}
		return one(func(pw string) string { return "ALTER ROLE " + role + " PASSWORD " + pw }), nil
	case "grant", "revoke":
		privs, err := keywords(c.Grant.Privs, ", ")
		if err != nil || privs == "" {
			return nil, fmt.Errorf("grant needs privileges")
		}
		// shortcut: splits at the first dot, so a schema name containing a dot is mis-split; upgrade: check the first part against the schema list.
		schema, table, ok := strings.Cut(c.Grant.Object, ".")
		if !ok {
			schema, table = "public", schema
		}
		obj := q(schema) + "." + q(table)
		if len(c.Grant.Columns) > 0 { // GRANT SELECT (a, b), UPDATE (a, b) ON ...
			cols := "(" + quoteList(c.Grant.Columns) + ")"
			privs = strings.ReplaceAll(privs, ", ", " "+cols+", ") + " " + cols
		}
		if c.Kind == "grant" {
			return []db.Statement{{Text: fmt.Sprintf("GRANT %s ON TABLE %s TO %s", privs, obj, role)}}, nil
		}
		return []db.Statement{{Text: fmt.Sprintf("REVOKE %s ON TABLE %s FROM %s", privs, obj, role)}}, nil
	}
	return nil, fmt.Errorf("unknown user change %q", c.Kind)
}

var _ db.UserAdmin = (*session)(nil)
