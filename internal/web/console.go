package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

const consoleCap = 1000

type consoleLine struct {
	Time string `json:"time"`
	Conn string `json:"conn"`
	Text string `json:"text"`
}

// consoleLog is the server-side console: every statement the web UI ran, newest last, capped.
type consoleLog struct {
	mu    sync.Mutex
	lines []consoleLine
}

func (c *consoleLog) add(conn, text string) {
	if len(text) > 64<<10 { // same rule as store.Console: a big statement becomes a note
		text = fmt.Sprintf("<%d KB statement>", len(text)>>10)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, consoleLine{time.Now().Format(time.RFC3339), conn, text})
	if len(c.lines) > consoleCap {
		c.lines = c.lines[len(c.lines)-consoleCap:]
	}
}

// tail is the console lines as text (crashlog keeps the last 20), for the crash log.
func (c *consoleLog) tail() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, l := range c.lines {
		out = append(out, l.Conn+": "+l.Text)
	}
	return out
}

func (s *Server) getConsole(r *http.Request) (any, error) {
	conn := r.URL.Query().Get("conn")
	s.con.mu.Lock()
	defer s.con.mu.Unlock()
	out := []consoleLine{}
	for _, l := range s.con.lines {
		if conn == "" || l.Conn == conn {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *Server) clearConsole(r *http.Request) (any, error) {
	s.con.mu.Lock()
	s.con.lines = nil
	s.con.mu.Unlock()
	return nil, nil
}
