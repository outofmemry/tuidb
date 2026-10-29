package web

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"webtuidb/internal/config"
)

// R-011: concurrent chat requests share one store without a race (run with -race).
func TestChatsStoreOnce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := New(Options{Cfg: config.Defaults(), Token: "t"})
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.chats() != nil {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 8 || s.chats() != s.chatStore {
		t.Fatal("chats()")
	}
}

func TestChatsCRUDAndSearch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	base := "/api/sessions/" + id + "/chats"
	c := e.ok("POST", base, nil)
	cid := c["ID"].(string)
	e.ok("PUT", base+"/"+cid, map[string]any{"Messages": []map[string]any{{"Role": "user", "Content": "How many Orders per day?"}, {"Role": "assistant", "Content": "select 1"}}})
	got := e.ok("GET", base+"/"+cid, nil)
	if got["Title"] != "How many Orders per day?" || len(got["Messages"].([]any)) != 2 {
		t.Fatalf("chat = %v", got)
	}
	if l := e.ok("GET", base+"?q=orders", nil)["chats"].([]any); len(l) != 1 {
		t.Fatalf("search hit = %v", l)
	}
	if l := e.ok("GET", base+"?q=zebra", nil)["chats"].([]any); len(l) != 0 {
		t.Fatalf("search miss = %v", l)
	}
	if code, _ := e.call("GET", base+"/..%2f..%2fx", nil, "tok"); code == 200 {
		t.Fatal("a path in the chat id must be refused")
	}
	e.ok("DELETE", base+"/"+cid, nil)
	if code, _ := e.call("GET", base+"/"+cid, nil, "tok"); code != 404 {
		t.Fatalf("deleted chat: %d", code)
	}
}

// P1-01: the chat endpoints speak Go names, and a Go-named body round-trips.
func TestChatsGoNames(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	e := newEnv(t, "")
	id := e.open()
	base := "/api/sessions/" + id + "/chats"
	c := e.ok("POST", base, nil)
	cid, _ := c["ID"].(string)
	if cid == "" {
		t.Fatalf("POST /chats = %v", c)
	}
	e.ok("PUT", base+"/"+cid, map[string]any{"Messages": []map[string]any{{"Role": "user", "Content": "hello there"}}})
	got := e.ok("GET", base+"/"+cid, nil)
	if got["ID"] != cid || len(got["Messages"].([]any)) != 1 || !strings.Contains(got["Title"].(string), "hello") {
		t.Fatalf("round trip = %v", got)
	}
	l := e.ok("GET", base, nil)["chats"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["ID"] != cid || l[0].(map[string]any)["Count"] == nil {
		t.Fatalf("list = %v", l)
	}
}
