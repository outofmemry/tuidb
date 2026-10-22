package db

import (
	"fmt"
	"time"
)

// FormatValue is how every grid and export prints a cell. NULL renders as "NULL".
func FormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case special:
		switch x {
		case Default:
			return "DEFAULT"
		case Empty:
			return ""
		}
		return "NULL"
	case Expr:
		return string(x)
	case string:
		return x
	case []byte:
		return fmt.Sprintf("\\x%x", x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05.999999")
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

// IsNull reports whether a pending-edit value means SQL NULL.
func IsNull(v any) bool { return v == nil || v == Null }
