package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	goredis "github.com/redis/go-redis/v9"

	"tuidb/internal/db"
)

// parseCommand splits one command line with redis-cli style quoting: double quotes honor
// \" \\ \n \r \t \b \a \xHH, single quotes are literal.
func parseCommand(line string) ([]string, error) {
	var args []string
	i, n := 0, len(line)
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
	for {
		for i < n && space(line[i]) {
			i++
		}
		if i >= n {
			return args, nil
		}
		var b []byte
		switch line[i] {
		case '"':
			i++
			closed := false
			for i < n && !closed {
				c := line[i]
				switch {
				case c == '\\' && i+1 < n:
					i++
					switch e := line[i]; {
					case e == 'x' && i+2 < n && unhex(line[i+1]) >= 0 && unhex(line[i+2]) >= 0:
						b = append(b, byte(unhex(line[i+1])<<4|unhex(line[i+2])))
						i += 2
					case e == 'n':
						b = append(b, '\n')
					case e == 'r':
						b = append(b, '\r')
					case e == 't':
						b = append(b, '\t')
					case e == 'b':
						b = append(b, '\b')
					case e == 'a':
						b = append(b, '\a')
					default:
						b = append(b, e)
					}
				case c == '"':
					closed = true
				default:
					b = append(b, c)
				}
				i++
			}
			if !closed {
				return nil, errors.New("unterminated double quote")
			}
		case '\'':
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated single quote")
			}
			b = []byte(line[i+1 : i+1+j])
			i += j + 2
		default:
			j := i
			for j < n && !space(line[j]) {
				j++
			}
			b = []byte(line[i:j])
			i = j
		}
		if i < n && !space(line[i]) {
			return nil, errors.New("closing quote must be followed by a space")
		}
		args = append(args, string(b))
	}
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// quoteArg renders s so parseCommand returns it unchanged.
func quoteArg(s string) string {
	bare := s != ""
	for _, r := range s {
		if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.:/@%+=,*", r))) {
			bare = false
			break
		}
	}
	if bare {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && w == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case !unicode.IsPrint(r):
			for _, c := range []byte(s[i : i+w]) {
				fmt.Fprintf(&b, `\x%02x`, c)
			}
		default:
			b.WriteRune(r)
		}
		i += w
	}
	b.WriteByte('"')
	return b.String()
}

func formatCommand(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = quoteArg(a)
	}
	return strings.Join(q, " ")
}

// Exec runs one command on a throwaway single-connection client: closing it is the only way to
// interrupt a blocking command (BLPOP, SUBSCRIBE...) when ctx is cancelled.
func (s *session) Exec(ctx context.Context, stmt string, limit int) (*db.Result, error) {
	args, err := parseCommand(stmt)
	if err != nil {
		return nil, fmt.Errorf("parse command: %w", err)
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	// Every Exec uses a fresh connection, so SELECT would be lost; make it switch the session instead.
	if strings.EqualFold(args[0], "SELECT") && len(args) == 2 {
		if err := s.UseDatabase(ctx, args[1]); err != nil {
			return nil, err
		}
		return &db.Result{Message: "OK, now using db " + args[1]}, nil
	}
	opt := s.opt
	s.mu.RLock()
	opt.DB = s.db
	s.mu.RUnlock()
	opt.PoolSize, opt.ReadTimeout = 1, -1
	cl := goredis.NewClient(&opt)
	defer cl.Close()

	start := time.Now()
	type reply struct {
		v   any
		err error
	}
	ch := make(chan reply, 1)
	go func() {
		v, err := cl.Do(ctx, toAny(args)...).Result()
		ch <- reply{v, err}
	}()
	var r reply
	select {
	case r = <-ch:
	case <-ctx.Done():
		cl.Close()
		<-ch
		return nil, ctx.Err()
	}
	res := &db.Result{}
	switch {
	case errors.Is(r.err, goredis.Nil):
		res.Message = "(nil)"
	case r.err != nil:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var re goredis.Error
		if errors.As(r.err, &re) {
			return nil, r.err
		}
		return nil, fmt.Errorf("exec %s: %w", strings.ToUpper(args[0]), r.err)
	default:
		render(res, r.v, limit)
	}
	res.Duration = time.Since(start)
	return res, nil
}

func render(res *db.Result, v any, limit int) {
	res.HasRows = true
	switch x := v.(type) {
	case nil:
		res.HasRows, res.Message = false, "(nil)"
	case string:
		if x == "OK" {
			res.HasRows, res.Message = false, x
			return
		}
		res.Columns, res.Rows = []db.ResultColumn{{Name: "value"}}, [][]any{{cell(x)}}
	case []any:
		res.Columns = []db.ResultColumn{{Name: "index", Type: "int"}, {Name: "value"}}
		for i, e := range x {
			res.Rows = append(res.Rows, []any{int64(i), replyCell(e)})
		}
		if len(x) == 0 {
			res.Message = "(empty array)"
		}
	case map[any]any:
		res.Columns = []db.ResultColumn{{Name: "key"}, {Name: "value"}}
		for k, e := range x {
			res.Rows = append(res.Rows, []any{fmt.Sprint(k), replyCell(e)})
		}
		sort.Slice(res.Rows, func(i, j int) bool { return res.Rows[i][0].(string) < res.Rows[j][0].(string) })
		if len(x) == 0 {
			res.Message = "(empty map)"
		}
	default:
		res.Columns, res.Rows = []db.ResultColumn{{Name: "value"}}, [][]any{{replyCell(v)}}
	}
	if limit > 0 && len(res.Rows) > limit {
		res.Rows, res.Truncated = res.Rows[:limit], true
	}
}

// replyCell normalizes one reply element: scalars stay typed, nested replies become JSON text.
func replyCell(v any) any {
	switch x := v.(type) {
	case nil, int64, float64, bool:
		return x
	case string:
		return cell(x)
	case error:
		return x.Error()
	}
	b, err := json.Marshal(jsonable(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func jsonable(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonable(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[fmt.Sprint(k)] = jsonable(e)
		}
		return out
	case error:
		return x.Error()
	}
	return v
}

// cell keeps valid UTF-8 as string and anything else as []byte.
func cell(s string) any {
	if utf8.ValidString(s) {
		return s
	}
	return []byte(s)
}

// str is the inverse of cell, also for numbers.
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	case float64:
		return fmt.Sprintf("%g", x)
	}
	return fmt.Sprint(v)
}

func jsonUnmarshal(s string, v any) error {
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("decode %q: %w", s, err)
	}
	return nil
}
