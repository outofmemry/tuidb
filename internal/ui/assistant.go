package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"webtuidb/internal/ai"
	"webtuidb/internal/config"
	"webtuidb/internal/db"
	"webtuidb/internal/keymap"
)

// assistantTab is the AI chat. The history lives in the workspace (m3.chat), so closing and
// reopening the tab keeps the conversation for the session.
type assistantTab struct {
	ws      *Workspace
	id      int
	in      lineEdit
	running bool
	cancel  context.CancelFunc
	back    int // lines scrolled up from the newest
	closed  bool
}

func (w *Workspace) assistantAction() tea.Cmd {
	for i, t := range w.tabs {
		if _, ok := t.(*assistantTab); ok { // one assistant per workspace: the chat and the running request are shared
			w.selectTab(i)
			return nil
		}
	}
	return w.addTab(&assistantTab{ws: w, id: w.newID()})
}

func (t *assistantTab) ID() int { return t.id }
func (t *assistantTab) Title() string {
	if p := t.ws.m3.provider; p != "" {
		return "AI assistant · " + p
	}
	return "AI assistant"
}
func (t *assistantTab) Dirty() bool                   { return false }
func (t *assistantTab) Context() string               { return keymap.Editor }
func (t *assistantTab) Init() tea.Cmd                 { return nil }
func (t *assistantTab) Handle(tea.Msg) tea.Cmd        { return nil }
func (t *assistantTab) Busy() bool                    { return t.running }
func (t *assistantTab) Action(string) (bool, tea.Cmd) { return false, nil }

func (t *assistantTab) Close() {
	t.closed = true
	if t.cancel != nil {
		t.cancel()
	}
}

// CancelWork stops the request in flight (prefix . from any pane, or Esc here).
func (t *assistantTab) CancelWork() tea.Cmd {
	if !t.running {
		return nil
	}
	t.cancel()
	return nil
}

func (t *assistantTab) Input(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		t.in.Handle(msg)
		return nil
	}
	switch k.Keystroke() {
	case "enter":
		return t.send()
	case "esc":
		return t.CancelWork()
	case "ctrl+o":
		return t.blocks()
	case "ctrl+l":
		return t.pickModel()
	case "ctrl+p":
		return t.pickProvider()
	case "ctrl+t":
		return t.messages()
	case "pgup":
		t.back += 10
	case "pgdown":
		t.back = max(0, t.back-10)
	case "up":
		t.back++
	case "down":
		t.back = max(0, t.back-1)
	default:
		t.in.Handle(msg)
	}
	return nil
}

// send asks the model. @table mentions put that table's DDL into the system prompt.
func (t *assistantTab) send() tea.Cmd {
	w, text := t.ws, strings.TrimSpace(t.in.Value())
	switch {
	case text == "":
		return nil
	case t.running:
		return w.a.say("the assistant is still answering (Esc stops it)")
	}
	cfg := t.aiCfg()
	if w.m3.model != "" {
		cfg.Model = w.m3.model
	}
	var refs []db.ObjRef
	var missed []string
	for _, n := range ai.Mentions(text) {
		found := false
		for _, o := range w.sb.objs {
			if (strings.EqualFold(o.Name, n) || strings.EqualFold(o.Schema+"."+o.Name, n)) && (o.Kind == db.KindTable || o.Kind == db.KindView || o.Kind == db.KindMatView) {
				refs, found = append(refs, o.ObjRef), true
				break
			}
		}
		if !found {
			missed = append(missed, n)
		}
	}
	w.m3.chat = append(w.m3.chat, ai.Message{Role: "user", Content: text})
	t.in.Set("")
	t.back, t.running = 0, true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.cancel = cancel
	msgs, sess, dialect, database := append([]ai.Message(nil), w.m3.chat...), w.sess, w.caps.Dialect, w.sess.CurrentDatabase()
	var note tea.Cmd
	if len(missed) > 0 {
		note = w.a.say("no table %s in this schema: mention skipped", strings.Join(missed, ", "))
	}
	return tea.Batch(note, liveCmd(func() func() tea.Cmd {
		defer cancel()
		key, kerr := config.ResolveAIKey(cfg)              // may run api_key_cmd: keep it off the UI loop
		if kerr == nil && key == "" && cfg.BaseURL == "" { // a local OpenAI-compatible server needs no key
			kerr = errNoAIKey
		}
		if kerr != nil {
			return func() tea.Cmd { return t.sendFailed(text, kerr, false) }
		}
		ddl := map[string]string{}
		for _, r := range refs {
			if d, err := sess.DDL(ctx, r); err == nil {
				ddl[r.Name] = d
			}
		}
		reply, err := ai.New(cfg, key).Chat(ctx, ai.SystemPrompt(dialect, database, ddl), msgs)
		stopped := ctx.Err() == context.Canceled
		return func() tea.Cmd {
			if err != nil {
				return t.sendFailed(text, err, stopped)
			}
			t.running = false
			if t.closed {
				return nil
			}
			w.m3.chat = append(w.m3.chat, ai.Message{Role: "assistant", Content: cleanReply(reply)})
			return nil
		}
	}))
}

var errNoAIKey = errors.New("the assistant needs an API key: set api_key or api_key_cmd under [ai] in the config")

// sendFailed takes the unanswered question back so the user can edit and resend. Only that entry is removed,
// and only if the messages dialog has not already deleted it.
func (t *assistantTab) sendFailed(text string, err error, stopped bool) tea.Cmd {
	w := t.ws
	t.running = false
	if t.closed {
		return nil
	}
	for i := len(w.m3.chat) - 1; i >= 0; i-- {
		if m := w.m3.chat[i]; m.Role == "user" && m.Content == text {
			w.m3.chat = append(w.m3.chat[:i:i], w.m3.chat[i+1:]...)
			break
		}
	}
	t.in.Set(text)
	switch {
	case stopped:
		return w.a.say("assistant: stopped")
	case errors.Is(err, errNoAIKey):
		return w.a.say("%s", err.Error())
	}
	return w.a.fail(fmt.Errorf("assistant: %w", err))
}

// cleanReply drops terminal control sequences (CSI / OSC) and control bytes from model output,
// keeping newlines and tabs, so a reply cannot write to the clipboard or move the cursor.
func cleanReply(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n"))
}

// blocks lists every ```sql block of the conversation: Enter runs it in a new query tab, c copies it.
func (t *assistantTab) blocks() tea.Cmd {
	w := t.ws
	var items []pickItem
	for _, m := range w.m3.chat {
		if m.Role != "assistant" {
			continue
		}
		for _, b := range ai.SQLBlocks(m.Content) {
			items = append(items, pickItem{Label: oneLine(b), Value: b})
		}
	}
	if len(items) == 0 {
		return w.a.say("no SQL blocks in the conversation yet")
	}
	// newest first
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	w.a.push(&pickerDlg{title: "SQL from the assistant", items: items, hint: "Enter: run in a new query tab   c: copy   Esc: close",
		keys: map[string]func(pickItem, bool) tea.Cmd{"c": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			return tea.Batch(setClip(it.Value.(string)), w.a.say("copied"))
		}},
		onPick: func(it pickItem, _ bool) tea.Cmd { return w.runSQL(it.Value.(string)) }})
	return nil
}

// runSQL opens sql in a new query tab and runs it through the normal path, so safe mode applies.
// A statement that changes data asks first.
func (w *Workspace) runSQL(sql string) tea.Cmd {
	run := func() tea.Cmd {
		cmd := w.newQueryTab(sql)
		if q, ok := w.tab().(*queryTab); ok {
			return tea.Batch(cmd, q.runAll())
		}
		return cmd
	}
	if db.IsReadOnly(w.caps.Dialect, sql) {
		return run()
	}
	w.a.push(&confirmDlg{title: "Run a statement that changes data?", lines: []string{sql}, yes: "Run", danger: true, onYes: run})
	return nil
}

func (t *assistantTab) View(th Theme, W, H int) string {
	var lines []string
	for _, m := range t.ws.m3.chat {
		if m.Role == "user" {
			lines = append(lines, "", th.Accent.Bold(true).Render(" you"))
			for _, l := range wrapText(m.Content, W-3) {
				lines = append(lines, "  "+l)
			}
			continue
		}
		lines = append(lines, "", th.OK.Bold(true).Render(" assistant"))
		inSQL := false
		for _, raw := range strings.Split(m.Content, "\n") {
			switch {
			case strings.HasPrefix(raw, "```"):
				inSQL = !inSQL && strings.HasPrefix(strings.ToLower(raw), "```sql")
				lines = append(lines, "  "+th.Dim.Render(raw))
				continue
			}
			for _, l := range wrapText(raw, W-3) {
				if inSQL {
					l = highlight(th, l, lexLines("postgres", []string{l})[0])
				}
				lines = append(lines, "  "+l)
			}
		}
		if n := len(ai.SQLBlocks(m.Content)); n > 0 {
			lines = append(lines, "  "+th.Hints("^o", fmt.Sprintf("copy or run the %d SQL block(s)", n)))
		}
	}
	if len(lines) == 0 {
		lines = []string{"", th.Dim.Render("  Ask about your data. Mention tables with @name to send their definition."), th.Dim.Render("  Enter sends · Esc stops · ^o copies or runs the SQL in a reply · ^l model · ^t messages (copy, resend, delete)")}
	}
	if t.running {
		lines = append(lines, "", th.Warn.Render("  "+t.ws.a.spinner()+" thinking…"))
	}
	room := max(1, H-2)
	t.back = clamp(t.back, 0, max(0, len(lines)-room))
	end := len(lines) - t.back
	body := lines[max(0, end-room):end]
	out := padLines(body, W, room)
	out = append(out, th.Border.Render(strings.Repeat("─", W)), th.Accent.Render("› ")+t.in.View(W-3, th, true))
	return strings.Join(out, "\n")
}
