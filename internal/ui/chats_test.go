package ui

import (
	"testing"

	"webtuidb/internal/store"
)

func TestM13NewChatSavesTheOldOne(t *testing.T) {
	h := newHarness(t, nil)
	chats := &store.Chats{Dir: t.TempDir()}
	h.a.d.Chats = chats
	h.connect()
	w := h.a.ws()
	w.panes.chat = append(w.panes.chat, aiMsg("user", "how many customers?"), aiMsg("assistant", "three"))
	h.run("assistant_new")
	list, _ := chats.List("shop")
	if len(list) != 1 || list[0].Title != "how many customers?" || list[0].Count != 2 {
		t.Fatalf("saved chats %+v", list)
	}
	if len(w.panes.chat) != 0 || w.panes.curChat != nil {
		t.Fatal("the new chat starts empty")
	}
	// history loads it back, clear keeps the id but empties the messages
	h.run("assistant_history")
	h.want("how many customers?")
	h.press("enter")
	if len(w.panes.chat) != 2 || w.panes.curChat == nil {
		t.Fatalf("loaded %d messages", len(w.panes.chat))
	}
	id := w.panes.curChat.ID
	h.run("assistant_clear")
	got, err := chats.Load("shop", id)
	if err != nil || len(got.Messages) != 0 || len(w.panes.chat) != 0 {
		t.Fatalf("cleared chat %+v %v", got, err)
	}
}

// P2-73: deleting a saved chat asks first.
func TestM16ChatDeleteAsksFirst(t *testing.T) {
	h := newHarness(t, nil)
	chats := &store.Chats{Dir: t.TempDir()}
	h.a.d.Chats = chats
	h.connect()
	w := h.a.ws()
	w.panes.chat = append(w.panes.chat, aiMsg("user", "how many customers?"), aiMsg("assistant", "three"))
	h.run("assistant_new")
	h.run("assistant_history")
	h.press("ctrl+x")
	if _, ok := h.a.overlays[len(h.a.overlays)-1].(*confirmDlg); !ok {
		t.Fatalf("no confirmation on top:\n%s", h.screen())
	}
	if l, _ := chats.List("shop"); len(l) != 1 {
		t.Fatal("deleted before the answer")
	}
	h.press("n")
	if l, _ := chats.List("shop"); len(l) != 1 {
		t.Fatal("deleted after no")
	}
	h.press("ctrl+x")
	h.press("y")
	if l, _ := chats.List("shop"); len(l) != 0 {
		t.Fatalf("not deleted after yes: %+v", l)
	}
}
