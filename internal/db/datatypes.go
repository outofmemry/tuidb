package db

import "strings"

// DataTypesOf lists the column types a structure editor can offer: the connection's Caps().Types (curated per
// engine, e.g. MariaDB's), else a per-dialect default (empty for mongo, redis and the like).
func DataTypesOf(sess Session) []string {
	c := sess.Caps()
	if len(c.Types) > 0 {
		return c.Types
	}
	return DefaultTypes(c.Dialect)
}

// DefaultTypes is the static type list for a dialect name.
// shortcut: static lists; querying pg_type / information_schema is the upgrade.
func DefaultTypes(dialect string) []string {
	switch dialect {
	case "postgres":
		return []string{"integer", "bigint", "smallint", "serial", "bigserial", "text", "varchar(255)", "char(1)", "boolean", "numeric(10,2)", "real", "double precision", "money", "date", "time", "timestamp", "timestamptz", "interval", "uuid", "json", "jsonb", "bytea", "inet", "cidr", "macaddr", "xml"}
	case "mysql", "mariadb":
		return strings.Fields("tinyint smallint mediumint int bigint decimal(10,2) float double bit boolean char(36) varchar(255) tinytext text mediumtext longtext binary(16) varbinary(255) tinyblob blob mediumblob longblob date time datetime timestamp year json enum('a','b') set('a','b')")
	case "clickhouse":
		return strings.Fields("UInt8 UInt16 UInt32 UInt64 Int8 Int16 Int32 Int64 Float32 Float64 Decimal(18,2) String FixedString(16) Date Date32 DateTime DateTime64(3) Bool UUID IPv4 IPv6 JSON Nullable(String) Array(String) LowCardinality(String)")
	}
	return nil
}
