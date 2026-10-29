package web

import (
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"webtuidb/internal/store"
)

var chatIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *Server) chats() *store.Chats {
	s.chatOnce.Do(func() { s.chatStore = store.DefaultChats() })
	return s.chatStore
}

func (s *Server) chatList(ss *session, r *http.Request) (any, error) {
	var l []store.ChatSummary
	var err error
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		l = s.chats().Search(ss.conn.Name, q)
	} else {
		l, err = s.chats().List(ss.conn.Name)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"chats": nonNil(l)}, nil
}

func (s *Server) chatNew(ss *session, r *http.Request) (any, error) {
	ch := s.chats().New(ss.conn.Name)
	if err := s.chats().Save(ss.conn.Name, ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatGet(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	ch, err := s.chats().Load(ss.conn.Name, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &apiErr{Code: http.StatusNotFound, Msg: "no such chat"}
		}
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatPut(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	var ch store.Chat
	if err := decode(r, &ch); err != nil {
		return nil, err
	}
	ch.ID = id // the URL names the chat, whatever the body says
	if len(ch.Messages) > 2000 {
		return nil, bad("a chat holds at most 2000 messages")
	}
	if err := s.chats().Save(ss.conn.Name, &ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Server) chatDelete(ss *session, r *http.Request) (any, error) {
	id := r.PathValue("cid")
	if !chatIDRe.MatchString(id) {
		return nil, bad("bad chat id")
	}
	return nil, s.chats().Delete(ss.conn.Name, id)
}
