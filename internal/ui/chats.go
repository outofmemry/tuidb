// chats.go: Saved assistant chats: store, persist, new, clear and history.

package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/ai"
	"webtuidb/internal/store"
)

func (w *Workspace) chatStore() *store.Chats {
	if w.a.d.Chats != nil {
		return w.a.d.Chats
	}
	return store.DefaultChats()
}

// persistChat writes the current conversation to its chat file (a no-op while there is nothing to keep).
func (w *Workspace) persistChat() tea.Cmd {
	m := &w.panes
	if len(m.chat) == 0 && m.curChat == nil {
		return nil
	}
	cs := w.chatStore()
	if m.curChat == nil {
		m.curChat = cs.New(w.conn.Name)
	}
	old := m.curChat.Messages
	msgs := make([]store.ChatMessage, len(m.chat))
	for i, c := range m.chat {
		msgs[i] = store.ChatMessage{Role: c.Role, Content: c.Content, At: time.Now()}
		if i < len(old) && old[i].Role == c.Role && old[i].Content == c.Content {
			msgs[i].At = old[i].At
		}
	}
	m.curChat.Messages = msgs
	if err := cs.Save(w.conn.Name, m.curChat); err != nil {
		return w.a.fail(fmt.Errorf("saving the chat: %w", err))
	}
	return nil
}

// assistantBusy is true while a request is in flight: new / clear / load would race with its reply.
func (w *Workspace) assistantBusy() bool {
	for _, t := range w.tabs {
		if a, ok := t.(*assistantTab); ok && a.running {
			return true
		}
	}
	return false
}

func (w *Workspace) assistantNew() tea.Cmd {
	if w.assistantBusy() {
		return w.a.say("the assistant is still answering (Esc stops it)")
	}
	save := w.persistChat()
	w.panes.chat, w.panes.curChat = nil, nil
	return tea.Batch(save, w.assistantAction(), w.a.say("new chat"))
}

func (w *Workspace) assistantClear() tea.Cmd {
	if w.assistantBusy() {
		return w.a.say("the assistant is still answering (Esc stops it)")
	}
	w.panes.chat = nil
	save := w.persistChat() // the chat keeps its id; its messages are gone
	return tea.Batch(save, w.a.say("chat cleared"))
}

func (w *Workspace) assistantHistory() tea.Cmd {
	if w.assistantBusy() {
		return w.a.say("the assistant is still answering (Esc stops it)")
	}
	cs, conn := w.chatStore(), w.conn.Name
	items := func(q string) []pickItem {
		list, _ := cs.List(conn)
		if q != "" {
			list = cs.Search(conn, q)
		}
		var out []pickItem
		for _, c := range list {
			out = append(out, pickItem{Label: orDefault(c.Title, "(empty chat)"), Detail: fmt.Sprintf("%d message(s) · %s", c.Count, c.Updated.Format("2006-01-02 15:04")), Value: c.ID})
		}
		return out
	}
	p := &pickerDlg{title: "Assistant chats", filter: true, dynamic: items, hint: "type to search  Enter: open  ctrl+x: delete  Esc: close", empty: "no saved chats"}
	p.items = items("")
	p.keys = map[string]func(pickItem, bool) tea.Cmd{"ctrl+x": func(it pickItem, ok bool) tea.Cmd {
		if !ok {
			return nil
		}
		id := it.Value.(string)
		w.a.push(&confirmDlg{title: "Delete chat " + strconv.Quote(it.Label) + "?", lines: []string{"The conversation file is removed."}, yes: "Delete", danger: true, onYes: func() tea.Cmd {
			if err := cs.Delete(conn, id); err != nil {
				return w.a.fail(err)
			}
			p.refresh()
			return nil
		}})
		return nil
	}}
	p.onPick = func(it pickItem, _ bool) tea.Cmd {
		if w.assistantBusy() {
			return w.a.say("the assistant is still answering (Esc stops it)")
		}
		ch, err := cs.Load(conn, it.Value.(string))
		if err != nil {
			return w.a.fail(err)
		}
		save := w.persistChat() // keep what is open before replacing it
		w.panes.chat = w.panes.chat[:0:0]
		for _, m := range ch.Messages {
			w.panes.chat = append(w.panes.chat, ai.Message{Role: m.Role, Content: m.Content})
		}
		w.panes.curChat = ch
		return tea.Batch(save, w.assistantAction(), w.a.say("loaded chat: %s", strings.TrimSpace(ch.Title)))
	}
	w.a.push(p)
	return nil
}
