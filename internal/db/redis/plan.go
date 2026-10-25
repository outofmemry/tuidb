package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"webtuidb/internal/db"
)

// lookup is what planning may ask the server: a key's type and a read command's reply.
type lookup struct {
	typ func(key string) (string, error)
	do  func(args ...string) (string, error)
}

func (s *session) lookup() lookup {
	call := func(f func(ctx context.Context) (string, error)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return f(ctx)
	}
	return lookup{
		typ: func(key string) (string, error) {
			return call(func(ctx context.Context) (string, error) { return s.c().Type(ctx, key).Result() })
		},
		do: func(args ...string) (string, error) {
			return call(func(ctx context.Context) (string, error) {
				v, err := s.c().Do(ctx, toAny(args)...).Result()
				return str(v), err // RESP3 returns ZSCORE as a double
			})
		},
	}
}

func (s *session) Plan(changes []db.Change) ([]db.Statement, error) {
	return planChanges(s.lookup(), changes)
}

func find(kvs []db.KeyVal, col string) (any, bool) {
	for _, kv := range kvs {
		if kv.Col == col {
			return kv.Val, true
		}
	}
	return nil, false
}

// cellText converts an edited cell to the string sent to redis.
func cellText(v any) (string, error) {
	switch x := v.(type) {
	case db.Expr:
		return string(x), nil
	case []byte:
		return string(x), nil
	}
	switch {
	case db.IsNull(v):
		return "", errors.New("redis has no NULL")
	case v == db.Default:
		return "", errors.New("redis has no DEFAULT")
	case v == db.Empty:
		return "", nil
	}
	return str(v), nil
}

func inferType(c db.Change) string {
	has := func(n string) bool {
		_, a := find(c.Key, n)
		_, b := find(c.Set, n)
		return a || b
	}
	switch {
	case has("field"):
		return "hash"
	case has("index"):
		return "list"
	case has("score"):
		return "zset"
	case has("member"):
		return "set"
	}
	return "string"
}

func planChanges(lk lookup, changes []db.Change) ([]db.Statement, error) {
	var out []db.Statement
	for _, c := range changes {
		key := c.Name
		typ, err := lk.typ(key)
		if err != nil {
			return nil, fmt.Errorf("type of %q: %w", key, err)
		}
		if typ == "none" {
			typ = inferType(c)
		}
		// get returns the new value of a column (Set) or, failing that, the original (Key).
		get := func(kvs []db.KeyVal, col string) (string, bool, error) {
			v, ok := find(kvs, col)
			if !ok {
				return "", false, nil
			}
			s, err := cellText(v)
			return s, true, err
		}
		need := func(kvs []db.KeyVal, col string) (string, error) {
			s, ok, err := get(kvs, col)
			if err == nil && !ok {
				err = fmt.Errorf("missing column %q", col)
			}
			return s, err
		}
		var cmds [][]string
		add := func(args ...string) { cmds = append(cmds, args) }
		err = func() error {
			switch typ {
			case "string":
				switch c.Kind {
				case db.Delete:
					add("DEL", key)
				default:
					v, err := need(c.Set, "value")
					if err != nil {
						return err
					}
					if c.Kind == db.Update {
						add("SET", key, v, "KEEPTTL")
					} else {
						add("SET", key, v)
					}
				}
			case "hash":
				switch c.Kind {
				case db.Insert:
					f, err := need(c.Set, "field")
					if err != nil {
						return err
					}
					v, _, err := get(c.Set, "value")
					if err != nil {
						return err
					}
					add("HSET", key, f, v)
				case db.Delete:
					f, err := need(c.Key, "field")
					if err != nil {
						return err
					}
					add("HDEL", key, f)
				default:
					old, err := need(c.Key, "field")
					if err != nil {
						return err
					}
					nf, ok, err := get(c.Set, "field")
					if err != nil {
						return err
					}
					if !ok {
						nf = old
					}
					v, hasV, err := get(c.Set, "value")
					if err != nil {
						return err
					}
					if nf == old {
						if !hasV {
							return errors.New("nothing to update")
						}
						add("HSET", key, nf, v)
						return nil
					}
					if e, err := lk.do("HEXISTS", key, nf); err != nil {
						return fmt.Errorf("check field %q: %w", nf, err)
					} else if e == "1" {
						return fmt.Errorf("field %q already exists", nf)
					}
					if !hasV { // renaming a field keeps its value
						if v, err = lk.do("HGET", key, old); err != nil {
							return fmt.Errorf("read value of field %q: %w", old, err)
						}
					}
					add("HDEL", key, old)
					add("HSET", key, nf, v)
				}
			case "list":
				switch c.Kind {
				case db.Insert:
					v, _, err := get(c.Set, "value")
					if err != nil {
						return err
					}
					add("RPUSH", key, v)
				case db.Delete:
					// LREM removes by value, which hits the first duplicate, not the row the user chose.
					// Overwrite that index with a unique marker and remove the marker instead.
					idx, err := need(c.Key, "index")
					if err != nil {
						return err
					}
					b := make([]byte, 8)
					if _, err := rand.Read(b); err != nil {
						return err
					}
					marker := "__webtuidb_deleted_" + hex.EncodeToString(b) + "__"
					add("LSET", key, idx, marker)
					add("LREM", key, "1", marker)
				default:
					idx, err := need(c.Key, "index")
					if err != nil {
						return err
					}
					if n, ok, _ := get(c.Set, "index"); ok && n != idx {
						return errors.New("list index cannot be edited")
					}
					v, err := need(c.Set, "value")
					if err != nil {
						return err
					}
					add("LSET", key, idx, v)
				}
			case "set":
				switch c.Kind {
				case db.Insert:
					m, err := need(c.Set, "member")
					if err != nil {
						return err
					}
					add("SADD", key, m)
				case db.Delete:
					m, err := need(c.Key, "member")
					if err != nil {
						return err
					}
					add("SREM", key, m)
				default:
					old, err := need(c.Key, "member")
					if err != nil {
						return err
					}
					m, err := need(c.Set, "member")
					if err != nil {
						return err
					}
					add("SREM", key, old)
					add("SADD", key, m)
				}
			case "zset":
				switch c.Kind {
				case db.Insert:
					m, err := need(c.Set, "member")
					if err != nil {
						return err
					}
					score, ok, err := get(c.Set, "score")
					if err != nil {
						return err
					}
					if !ok {
						score = "0"
					}
					add("ZADD", key, score, m)
				case db.Delete:
					m, err := need(c.Key, "member")
					if err != nil {
						return err
					}
					add("ZREM", key, m)
				default:
					old, err := need(c.Key, "member")
					if err != nil {
						return err
					}
					m, ok, err := get(c.Set, "member")
					if err != nil {
						return err
					}
					if !ok {
						m = old
					}
					score, hasS, err := get(c.Set, "score")
					if err != nil {
						return err
					}
					if m == old {
						if !hasS {
							return errors.New("nothing to update")
						}
						add("ZADD", key, score, m)
						return nil
					}
					if _, err := lk.do("ZSCORE", key, m); err == nil {
						return fmt.Errorf("member %q already exists", m)
					} else if !errors.Is(err, goredis.Nil) {
						return fmt.Errorf("check member %q: %w", m, err)
					}
					if !hasS {
						if score, err = lk.do("ZSCORE", key, old); err != nil {
							return fmt.Errorf("read score of %q: %w", old, err)
						}
					}
					add("ZREM", key, old)
					add("ZADD", key, score, m)
				}
			default:
				return fmt.Errorf("a %s key is read-only", typ)
			}
			return nil
		}()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for _, a := range cmds {
			out = append(out, db.Statement{Text: formatCommand(a...)})
		}
	}
	return out, nil
}

func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (s *session) PlanAdmin(op db.AdminOp, ref db.ObjRef, opts map[string]string) ([]db.Statement, error) {
	st := func(args ...string) ([]db.Statement, error) {
		return []db.Statement{{Text: formatCommand(args...)}}, nil
	}
	switch op {
	case db.OpDropObject, db.OpTruncate:
		return st("DEL", ref.Name)
	case db.OpRenameObject:
		if opts["new_name"] == "" {
			return nil, errors.New("rename needs new_name")
		}
		// RENAME silently overwrites an existing destination key
		if s.c() != nil { // planning without a server (unit tests) has nothing to check
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if n, err := s.c().Exists(ctx, opts["new_name"]).Result(); err != nil {
				return nil, err
			} else if n > 0 {
				return nil, fmt.Errorf("key %q already exists", opts["new_name"])
			}
		}
		return st("RENAME", ref.Name, opts["new_name"])
	case db.OpClone:
		if opts["new_name"] == "" {
			return nil, errors.New("clone needs new_name")
		}
		if !truthy(opts["with_data"]) {
			return nil, errors.New("redis cannot clone a key without its data")
		}
		return st("COPY", ref.Name, opts["new_name"])
	case db.OpDropDatabase:
		if ref.Name != "" && ref.Name != s.CurrentDatabase() {
			return nil, fmt.Errorf("switch to db %s first: FLUSHDB only empties the current db", ref.Name)
		}
		return st("FLUSHDB")
	}
	return nil, fmt.Errorf("redis does not support %s", op)
}

func (s *session) Apply(ctx context.Context, stmts []db.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	all := make([][]string, len(stmts))
	for i, st := range stmts {
		args, err := parseCommand(st.Text)
		if err != nil || len(args) == 0 {
			return fmt.Errorf("statement %d %q: invalid command: %v", i+1, st.Text, err)
		}
		all[i] = args
	}
	// MULTI/EXEC only aborts on queue-time errors, so check each key still has the type the plan
	// assumed, under WATCH so a change after the check aborts EXEC.
	want := map[string]string{}
	var keys []string
	for _, a := range all {
		if len(a) < 2 {
			continue
		}
		if _, ok := want[a[1]]; !ok {
			want[a[1]] = cmdType[strings.ToUpper(a[0])] // first statement's assumption wins
			keys = append(keys, a[1])
		}
	}
	err := s.c().Watch(ctx, func(tx *goredis.Tx) error {
		for _, k := range keys {
			if want[k] == "" {
				continue
			}
			typ, err := tx.Type(ctx, k).Result()
			if err != nil {
				return fmt.Errorf("type of %q: %w", k, err)
			}
			if typ != "none" && typ != want[k] {
				return fmt.Errorf("key %q is now a %s, not a %s; nothing was applied", k, typ, want[k])
			}
		}
		cmds, err := tx.TxPipelined(ctx, func(p goredis.Pipeliner) error {
			for _, a := range all {
				p.Do(ctx, toAny(a)...)
			}
			return nil
		})
		if errors.Is(err, goredis.TxFailedErr) {
			return errors.New("a key changed while applying; nothing was applied")
		}
		if err == nil || errors.Is(err, goredis.Nil) {
			return nil
		}
		var done, failed []string
		for i, c := range cmds {
			if e := c.Err(); e != nil && !errors.Is(e, goredis.Nil) {
				failed = append(failed, fmt.Sprintf("statement %d %q: %v", i+1, stmts[i].Text, e))
			} else {
				done = append(done, strconv.Itoa(i+1))
			}
		}
		if len(done) == 0 {
			return fmt.Errorf("%w; nothing was applied", err)
		}
		return fmt.Errorf("%s; statements %s were applied and cannot be rolled back", strings.Join(failed, "; "), strings.Join(done, ", "))
	}, keys...)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	return nil
}

// cmdType is the key type a planned write command needs ("": any type).
var cmdType = map[string]string{
	"SET": "string", "HSET": "hash", "HDEL": "hash", "RPUSH": "list", "LSET": "list", "LREM": "list",
	"SADD": "set", "SREM": "set", "ZADD": "zset", "ZREM": "zset",
}
