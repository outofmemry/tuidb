package store

import (
	"fmt"
	"sync"
	"time"
)

const (
	consoleMax     = 1000
	consoleMaxStmt = 64 << 10
)

// ConsoleLine is one executed statement in the console log.
type ConsoleLine struct {
	At     time.Time
	SQL    string
	Failed bool
	Dur    time.Duration
}

// Console is the log of the last 1000 statements a workspace ran, shared by both UIs.
type Console struct {
	mu    sync.Mutex
	lines []ConsoleLine
}

// Add appends a statement; one over 64 KB is replaced by "<N KB statement>" so the log stays small.
func (c *Console) Add(sql string, failed bool, dur time.Duration) {
	if len(sql) > consoleMaxStmt {
		sql = fmt.Sprintf("<%d KB statement>", len(sql)>>10)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, ConsoleLine{At: time.Now(), SQL: sql, Failed: failed, Dur: dur})
	if len(c.lines) > consoleMax {
		c.lines = append([]ConsoleLine(nil), c.lines[len(c.lines)-consoleMax:]...)
	}
}

// Lines returns a copy, oldest first.
func (c *Console) Lines() []ConsoleLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ConsoleLine(nil), c.lines...)
}

func (c *Console) Clear() {
	c.mu.Lock()
	c.lines = nil
	c.mu.Unlock()
}
