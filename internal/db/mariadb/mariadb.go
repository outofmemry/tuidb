// Package mariadb is the MariaDB driver. MariaDB speaks the MySQL protocol and the MySQL dialect, so
// it reuses the session code of internal/db/mysql (which detects the server flavour) and only differs
// in what it advertises: sequences and MariaDB's column types.
package mariadb

import (
	"strings"

	"tuidb/internal/config"
	"tuidb/internal/db"
	"tuidb/internal/db/mysql"
)

func init() { db.Register(mysql.New(config.MariaDB, adjust)) }

// types are the column types of MariaDB 11.x, including the native UUID, INET4 and INET6 types.
var types = strings.Fields(`tinyint smallint mediumint int bigint decimal(10,2) float double bit(1) boolean
	char(36) varchar(255) tinytext text mediumtext longtext binary(16) varbinary(255) tinyblob blob mediumblob longblob
	enum('a','b') set('a','b') date time datetime timestamp year json uuid inet4 inet6 point linestring polygon geometry`)

func adjust(c *db.Capabilities) {
	c.Sequences = true
	c.Types = types
}
