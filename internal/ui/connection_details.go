// connection_details.go: Connection details: TLS state, the details dialog and the colour picker.

package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// statusColours are the swatches of the colour picker; the tag's own colour comes back with "default".
var statusColours = []struct{ name, hex string }{
	{"red", "#e06c75"}, {"orange", "#d19a66"}, {"yellow", "#e5c07b"}, {"green", "#98c379"},
	{"teal", "#56b6c2"}, {"blue", "#61afef"}, {"purple", "#c678dd"}, {"grey", "#8b919b"},
}

func tlsState(c string) string {
	if c == "" || c == "disable" {
		return "off"
	}
	return "on (" + c + ")"
}

func (w *Workspace) connectionDetails() tea.Cmd {
	c := w.conn
	addr := c.Host
	if c.Port != 0 || c.Host != "" {
		addr = fmt.Sprintf("%s:%d", c.Host, c.EffectivePort())
	}
	lines := []string{
		"Name:      " + c.Name,
		"Driver:    " + c.Driver,
		"Server:    " + w.sess.ServerInfo(),
		"Host:      " + orDefault(addr, "-"),
		"Database:  " + w.sess.CurrentDatabase(),
		"User:      " + orDefault(c.User, "-"),
		"TLS:       " + tlsState(c.SSLMode),
		"Safe mode: " + w.level.Label(),
		"Read only: " + strings.ToLower(yesNo(c.ReadOnly)),
		"Colour:    " + orDefault(c.Color, "(the tag's)"),
	}
	if c.SSH != nil && c.SSH.Host != "" {
		lines = append(lines, fmt.Sprintf("SSH:       %s@%s:%d", c.SSH.User, c.SSH.Host, orInt(c.SSH.Port, 22)))
	}
	if c.SocksProxy != "" {
		lines = append(lines, "SOCKS:     "+c.SocksProxy)
	}
	if c.Source != "" {
		lines = append(lines, "Source:    "+c.Source)
	}
	w.a.push(&textViewDlg{title: "Connection details", lines: lines, actions: []viewAction{{Key: "o", Label: "o: colour", Do: w.pickColour}}})
	return nil
}

// pickColour saves the status colour of the connection (empty restores the tag's colour).
func (w *Workspace) pickColour() tea.Cmd {
	items := []pickItem{{Label: "default", Detail: "the tag's colour", Value: ""}}
	for _, s := range statusColours {
		items = append(items, pickItem{Label: s.name, Detail: s.hex, Value: s.hex})
	}
	items = append(items, pickItem{Label: "custom…", Value: "\x00custom"})
	set := func(hex string) tea.Cmd {
		w.conn.Color = hex
		for i := range w.a.d.Conns {
			if w.a.d.Conns[i].Name == w.conn.Name {
				w.a.d.Conns[i].Color = hex
			}
		}
		if w.conn.Source != "" {
			return w.a.say("colour set for this session (%s comes from %s)", w.conn.Name, w.conn.Source)
		}
		if err := w.a.saveConns(); err != nil {
			return w.a.fail(err)
		}
		return w.a.say("colour saved")
	}
	w.a.push(&pickerDlg{title: "Status colour of " + w.conn.Name, items: items, onPick: func(it pickItem, _ bool) tea.Cmd {
		hex := it.Value.(string)
		if hex != "\x00custom" {
			return set(hex)
		}
		f := newForm("Custom colour", []formField{{Key: "hex", Label: "Hex", Kind: fText, Value: w.conn.Color, Hint: "#rrggbb"}}, func(v map[string]string) tea.Cmd {
			return set("#" + strings.TrimPrefix(strings.TrimSpace(v["hex"]), "#")) // stored as # + 6 hex, like the web
		})
		f.validate = func(v map[string]string) error {
			h := strings.TrimPrefix(strings.TrimSpace(v["hex"]), "#")
			if len(h) != 6 {
				return fmt.Errorf("a colour is six hex digits, like #61afef")
			}
			for _, r := range h {
				if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
					return fmt.Errorf("a colour is six hex digits, like #61afef")
				}
			}
			return nil
		}
		w.a.push(f)
		return nil
	}})
	return nil
}
