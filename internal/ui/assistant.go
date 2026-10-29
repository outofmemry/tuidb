package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/anthropic-sdk-go"
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
	if p := t.ws.panes.provider; p != "" {
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
	if w.panes.model != "" {
		cfg.Model = w.panes.model
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
	w.panes.chat = append(w.panes.chat, ai.Message{Role: "user", Content: text})
	t.in.Set("")
	t.back, t.running = 0, true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.cancel = cancel
	msgs, sess, dialect, database := append([]ai.Message(nil), w.panes.chat...), w.sess, w.caps.Dialect, w.sess.CurrentDatabase()
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
			w.panes.chat = append(w.panes.chat, ai.Message{Role: "assistant", Content: cleanReply(reply)})
			return w.persistChat()
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
	for i := len(w.panes.chat) - 1; i >= 0; i-- {
		if m := w.panes.chat[i]; m.Role == "user" && m.Content == text {
			w.panes.chat = append(w.panes.chat[:i:i], w.panes.chat[i+1:]...)
			break
		}
	}
	t.in.Set(text)
	seq := w.a.toastSeq
	save := w.persistChat() // a failed save must be reported too, not overwritten by the send error's toast
	note := ""
	if w.a.toastSeq != seq && w.a.toastErr {
		note = "; " + w.a.toast
	}
	switch {
	case stopped:
		if note != "" {
			return tea.Batch(save, w.a.fail(fmt.Errorf("assistant: stopped%s", note)))
		}
		return tea.Batch(save, w.a.say("assistant: stopped"))
	case errors.Is(err, errNoAIKey): // a refusal: ✗, like every other one (T-P3-01)
		return tea.Batch(save, w.a.fail(fmt.Errorf("%s%s", err.Error(), note)))
	}
	return tea.Batch(save, w.a.fail(fmt.Errorf("assistant: %w%s", err, note)))
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
	for _, m := range w.panes.chat {
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
	for _, m := range t.ws.panes.chat {
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
	rule := th.Border.Render(strings.Repeat("─", W))
	if stop := th.Hints("Esc", "Stop"); t.running && W >= 2+width(stop) { // a pane narrower than the label keeps the plain rule
		rule = th.Border.Render("──") + stop + th.Border.Render(strings.Repeat("─", max(0, W-2-width(stop))))
	}
	out = append(out, rule, th.Accent.Render("› ")+t.in.View(W-3, th, true))
	return strings.Join(out, "\n")
}

// pickModel lists the provider's models (OpenAI-compatible: GET /models; Anthropic: the current ids) and
// uses the pick for this session. shortcut: ACP custom agents and Copilot / Codex sign-in are not here.
func (t *assistantTab) pickModel() tea.Cmd {
	w := t.ws
	cfg := t.aiCfg()
	show := func(ids []string) {
		var items []pickItem
		for _, id := range ids {
			d := ""
			if id == orDefault(w.panes.model, cfg.Model) {
				d = "current"
			}
			items = append(items, pickItem{Label: id, Detail: d, Value: id})
		}
		w.a.push(&pickerDlg{title: "Assistant model (this session)", filter: true, items: items, empty: "the provider returned no models",
			onPick: func(it pickItem, _ bool) tea.Cmd {
				w.panes.model = it.Value.(string)
				return w.a.say("assistant model: %s", w.panes.model)
			}})
	}
	if cfg.Provider != "openai" {
		show([]string{string(anthropic.ModelClaudeOpus5_5), string(anthropic.ModelClaudeSonnet5_5), string(anthropic.ModelClaudeHaiku5_5)})
		return nil
	}
	return liveCmd(func() func() tea.Cmd {
		key, err := config.ResolveAIKey(cfg) // may run api_key_cmd: keep it off the UI loop
		var ids []string
		if err == nil {
			ids, err = openAIModels(cfg, key)
		}
		return func() tea.Cmd {
			if err != nil {
				return w.a.fail(fmt.Errorf("models: %w", err))
			}
			show(ids)
			return nil
		}
	})
}

// openAIModels asks GET <base_url>/models for the model ids.
func openAIModels(cfg config.AI, key string) ([]string, error) {
	base := orDefault(cfg.BaseURL, "https://api.openai.com/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	var out struct{ Data []struct{ ID string } }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// messages lists the conversation, newest first: Enter resends a question (copies an answer), c copies, d deletes.
func (t *assistantTab) messages() tea.Cmd {
	w := t.ws
	p := &pickerDlg{title: "Assistant messages", hint: "Enter: resend (copy an answer)   c: copy   d: delete   Esc: close", empty: "no messages yet"}
	rebuild := func() {
		p.items = nil
		for i := len(w.panes.chat) - 1; i >= 0; i-- {
			m := w.panes.chat[i]
			p.items = append(p.items, pickItem{Label: m.Role + ": " + oneLine(m.Content), Value: i})
		}
		p.sel = clamp(p.sel, 0, max(0, len(p.items)-1))
	}
	rebuild()
	p.keys = map[string]func(pickItem, bool) tea.Cmd{
		"c": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			return w.a.copyText(w.panes.chat[it.Value.(int)].Content, "message")
		},
		"d": func(it pickItem, ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			i := it.Value.(int)
			if t.running && i >= len(w.panes.chat)-1 {
				return w.a.say("the assistant is still answering that question (Esc stops it)")
			}
			if i >= len(w.panes.chat) {
				return nil
			}
			w.panes.chat = append(w.panes.chat[:i:i], w.panes.chat[i+1:]...)
			rebuild()
			return nil
		},
	}
	p.onPick = func(it pickItem, _ bool) tea.Cmd {
		i := it.Value.(int)
		m := w.panes.chat[i]
		if m.Role != "user" {
			return w.a.copyText(m.Content, "answer")
		}
		if t.running {
			return w.a.say("the assistant is still answering (Esc stops it)")
		}
		w.panes.chat = w.panes.chat[:i] // the question and everything after it go; send asks again
		t.in.Set(m.Content)
		return t.send()
	}
	w.a.push(p)
	return nil
}

// aiCfg is the assistant settings for this chat's provider (the default when none was picked).
func (t *assistantTab) aiCfg() config.AI {
	c, err := config.AIFor(t.ws.a.d.Cfg.AI, t.ws.panes.provider)
	if err != nil {
		c, _ = config.AIFor(t.ws.a.d.Cfg.AI, "")
	}
	return c
}

// pickProvider lists the default first, then the [ai.providers.<name>] entries.
func (t *assistantTab) pickProvider() tea.Cmd {
	w := t.ws
	items := []pickItem{{Label: "default", Detail: w.a.d.Cfg.AI.Provider + " " + w.a.d.Cfg.AI.Model, Value: ""}}
	for _, n := range config.AIProviderNames(w.a.d.Cfg.AI) {
		c, _ := config.AIFor(w.a.d.Cfg.AI, n)
		items = append(items, pickItem{Label: n, Detail: c.Provider + " " + c.Model, Value: n})
	}
	for i := range items {
		if items[i].Value.(string) == w.panes.provider {
			items[i].Label = "• " + items[i].Label
		}
	}
	w.a.push(&pickerDlg{title: "Assistant provider (this chat)", items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		w.panes.provider, w.panes.model = it.Value.(string), "" // the model of the old provider does not carry over
		return w.a.say("assistant provider: %s", orDefault(w.panes.provider, "default"))
	}})
	return nil
}
