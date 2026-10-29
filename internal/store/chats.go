package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"webtuidb/internal/config"
)

type ChatMessage struct {
	Role    string    `json:"Role"`
	Content string    `json:"Content"`
	At      time.Time `json:"At"`
}

type Chat struct {
	ID       string        `json:"ID"`
	Title    string        `json:"Title"`
	Created  time.Time     `json:"Created"`
	Updated  time.Time     `json:"Updated"`
	Messages []ChatMessage `json:"Messages"`
}

type ChatSummary struct {
	ID      string    `json:"ID"`
	Title   string    `json:"Title"`
	Updated time.Time `json:"Updated"`
	Count   int       `json:"Count"`
}

// Chats keeps one JSON file per assistant conversation: <Dir>/<connection>/<id>.json.
type Chats struct{ Dir string }

// DefaultChats is the chat store under the data dir.
func DefaultChats() *Chats { return &Chats{Dir: filepath.Join(config.DataDir(), "chats")} }

// maxChatBytes caps one chat file: Save refuses more, scan skips bigger files (a pasted 50 MB message must not hang the picker).
const maxChatBytes = 4 << 20

var errChatTooBig = errors.New("chat too large to save (over 4 MiB)")
var errBadConn = errors.New("bad connection name for chats")

func (c *Chats) dir(conn string) string {
	h := History{Dir: c.Dir}
	return strings.TrimSuffix(h.path(conn), ".jsonl") // same safe-name encoding as history
}

var errBadID = errors.New("bad chat id")

func (c *Chats) file(conn, id string) (string, error) {
	if conn == "" || conn == "." || conn == ".." { // "chats/.." would be the data dir itself
		return "", errBadConn
	}
	if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\\`) || strings.HasPrefix(id, ".") {
		return "", errBadID
	}
	return filepath.Join(c.dir(conn), id+".json"), nil
}

// New returns an unsaved chat with a fresh id.
func (c *Chats) New(conn string) *Chat {
	var b [8]byte
	_, _ = rand.Read(b[:])
	now := time.Now()
	return &Chat{ID: hex.EncodeToString(b[:]), Created: now, Updated: now, Messages: []ChatMessage{}}
}

// chatTitle is the first user message cut to 60 runes, one line.
func chatTitle(ch *Chat) string {
	for _, m := range ch.Messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			t := strings.Join(strings.Fields(m.Content), " ")
			if r := []rune(t); len(r) > 60 {
				t = string(r[:60])
			}
			return t
		}
	}
	return ""
}

// Save writes ch atomically; it sets Title (from the first user message) when empty and refreshes Updated.
func (c *Chats) Save(conn string, ch *Chat) error {
	path, err := c.file(conn, ch.ID)
	if err != nil {
		return err
	}
	if ch.Title == "" {
		ch.Title = chatTitle(ch)
	}
	if ch.Created.IsZero() {
		ch.Created = time.Now()
	}
	ch.Updated = time.Now()
	if ch.Messages == nil {
		ch.Messages = []ChatMessage{}
	}
	data, err := json.Marshal(ch)
	if err != nil {
		return err
	}
	if len(data) > maxChatBytes {
		return errChatTooBig
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data)
}

func (c *Chats) Load(conn, id string) (*Chat, error) {
	path, err := c.file(conn, id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ch Chat
	if err := json.Unmarshal(b, &ch); err != nil {
		return nil, err
	}
	ch.ID = id
	return &ch, nil
}

func (c *Chats) Delete(conn, id string) error {
	path, err := c.file(conn, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// List returns the connection's chats, newest first; unreadable files are skipped.
func (c *Chats) List(conn string) ([]ChatSummary, error) { return c.scan(conn, ""), nil }

// Search keeps chats whose title or any message contains q (case-insensitive).
func (c *Chats) Search(conn, q string) []ChatSummary { return c.scan(conn, strings.ToLower(q)) }

// shortcut: Search reads every chat file of the connection; upgrade to an index if histories grow large.
func (c *Chats) scan(conn, q string) []ChatSummary {
	if _, err := c.file(conn, "x"); err != nil {
		return []ChatSummary{}
	}
	files, _ := filepath.Glob(filepath.Join(c.dir(conn), "*.json"))
	out := []ChatSummary{}
	for _, f := range files {
		if st, err := os.Stat(f); err != nil || st.Size() > maxChatBytes {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		ch, err := c.Load(conn, id)
		if err != nil {
			continue
		}
		if q != "" {
			hit := strings.Contains(strings.ToLower(ch.Title), q)
			for _, m := range ch.Messages {
				hit = hit || strings.Contains(strings.ToLower(m.Content), q)
			}
			if !hit {
				continue
			}
		}
		out = append(out, ChatSummary{ID: ch.ID, Title: ch.Title, Updated: ch.Updated, Count: len(ch.Messages)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}
