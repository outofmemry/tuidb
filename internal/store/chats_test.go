package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// P1-01: the web client reads Go-named keys.
func TestChatJSONUsesGoNames(t *testing.T) {
	b, _ := json.Marshal(Chat{ID: "a", Messages: []ChatMessage{{Role: "user"}}})
	for _, k := range []string{`"ID"`, `"Title"`, `"Messages"`, `"Role"`, `"Content"`, `"At"`, `"Updated"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("missing %s in %s", k, b)
		}
	}
	s, _ := json.Marshal(ChatSummary{})
	if !strings.Contains(string(s), `"Count"`) {
		t.Fatal(string(s))
	}
	var old Chat // files written with lowercase keys still load
	if err := json.Unmarshal([]byte(`{"id":"x","title":"t","messages":[{"role":"user","content":"hi"}]}`), &old); err != nil || old.ID != "x" || old.Messages[0].Content != "hi" {
		t.Fatalf("%+v %v", old, err)
	}
}

// P2-17: oversized chats are refused on save and skipped on scan.
func TestChatSizeCap(t *testing.T) {
	c := &Chats{Dir: t.TempDir()}
	big := c.New("c")
	big.Messages = append(big.Messages, ChatMessage{Role: "user", Content: strings.Repeat("x", maxChatBytes+1)})
	if err := c.Save("c", big); err == nil {
		t.Fatal("oversized chat saved")
	}
	ok := c.New("c")
	ok.Messages = append(ok.Messages, ChatMessage{Role: "user", Content: "hi"})
	if err := c.Save("c", ok); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(c.dir("c"), "huge.json"), []byte(`{"ID":"huge","Title":"`+strings.Repeat("y", maxChatBytes)+`"}`), 0o600)
	if got := c.Search("c", "y"); len(got) != 0 {
		t.Fatalf("oversized file scanned: %v", got)
	}
	if got, _ := c.List("c"); len(got) != 1 {
		t.Fatalf("list %v", got)
	}
}

// P2-18: "." and ".." must not become the chat directory.
func TestChatsRejectDotNames(t *testing.T) {
	c := &Chats{Dir: filepath.Join(t.TempDir(), "chats")}
	for _, n := range []string{"", ".", ".."} {
		ch := c.New(n)
		if err := c.Save(n, ch); err == nil {
			t.Fatalf("saved under %q", n)
		}
		if got := c.Search(n, ""); len(got) != 0 {
			t.Fatalf("scan %q: %v", n, got)
		}
	}
}

// P2-19: a >64 KB statement re-imported does not duplicate.
func TestAppendAllDedupesClippedSQL(t *testing.T) {
	h := &History{Dir: t.TempDir()}
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	es := []HistoryEntry{{Time: ts, SQL: "select '" + strings.Repeat("a", maxSQLBytes+100) + "'"}}
	if n, err := h.AppendAll("c", es); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := h.List("c", "", 0)
	// the second import comes from the stored (already clipped) text
	if n, err := h.AppendAll("c", got); err != nil || n != 0 {
		t.Fatalf("clipped re-import n=%d err=%v", n, err)
	}
	for i := range got {
		got[i].ID = ""
	}
	if n, err := h.AppendAll("c", got); err != nil || n != 0 {
		t.Fatalf("id-less re-import n=%d err=%v", n, err)
	}
	if n, err := h.AppendAll("c", es); err != nil || n != 0 {
		t.Fatalf("unclipped re-import n=%d err=%v", n, err)
	}
}
