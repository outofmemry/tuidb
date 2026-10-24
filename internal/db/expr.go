package db

import (
	"fmt"
	"strings"
)

// CheckExpr accepts one SQL expression for Set Value > Advanced: the planner inlines it, so it must not be
// able to end the statement or start a comment, and its quotes and parentheses must balance. It is the same
// text the user could send through the editor, which is gated by safe mode the same way.
func CheckExpr(e string) error {
	e = strings.TrimSpace(e)
	if e == "" || len(e) > 1000 {
		return fmt.Errorf("an expression of 1 to 1000 characters is required")
	}
	depth := 0
	for i := 0; i < len(e); i++ {
		switch c := e[i]; {
		case c == '\'' || c == '"':
			j := i + 1
			for ; j < len(e); j++ {
				if e[j] == c {
					if j+1 < len(e) && e[j+1] == c { // doubled quote: an escaped quote
						j++
						continue
					}
					break
				}
			}
			if j >= len(e) {
				return fmt.Errorf("expression %q has an unclosed quote", e)
			}
			i = j
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth < 0 {
				return fmt.Errorf("expression %q has an unbalanced parenthesis", e)
			}
		case c == ';' || c == '\\' || c == '`' || c == '$' && i+1 < len(e) && e[i+1] == '$',
			c == '-' && i+1 < len(e) && e[i+1] == '-', c == '/' && i+1 < len(e) && e[i+1] == '*', c == '#':
			return fmt.Errorf("expression %q may not contain %q", e, string(c))
		}
	}
	if depth != 0 {
		return fmt.Errorf("expression %q has an unbalanced parenthesis", e)
	}
	return nil
}
