package web

import (
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"webtuidb/internal/config"
	"webtuidb/internal/keymap"

	"github.com/BurntSushi/toml"
)

func (s *Server) settingsSchema(r *http.Request) (any, error) {
	return map[string]any{"fields": schemaFields(s.cfg()), "path": s.configPath()}, nil
}

// schemaField is a settings field plus whether the browser may write it.
type schemaField struct {
	config.Field
	ReadOnly bool `json:"readOnly,omitempty"`
}

// schemaFields marks the path settings read-only, so one of them cannot fail a whole save.
func schemaFields(c config.Config) []schemaField {
	fs := config.Schema(c)
	out := make([]schemaField, len(fs))
	for i, f := range fs {
		out[i] = schemaField{Field: f, ReadOnly: slices.Contains(pathSettings, f.Key)}
	}
	return out
}

// pathSettings are the settings the browser may not change: they point the server at files or folders.
var pathSettings = []string{"queries.shared_dirs", "theme.file", "web.tls_cert", "web.tls_key", "mcp.tls_cert", "mcp.tls_key"}

const commentsWarning = "comments in config.toml were not kept"

// putSettings writes {values: {"section.key": "text"}} to config.toml after validating it, comments are not kept.
func (s *Server) putSettings(r *http.Request) (any, error) {
	var in struct{ Values map[string]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if len(in.Values) == 0 {
		return nil, bad("no values to save")
	}
	for _, k := range pathSettings { // these name files or folders on the server: only config.toml itself may set them
		if _, ok := in.Values[k]; ok {
			return nil, bad("%s names a path on the server and can only be edited in config.toml", k)
		}
	}
	had := config.HasComments(s.configPath())
	if err := config.ApplyValues(s.configPath(), in.Values); err != nil {
		return nil, bad("%v", err)
	}
	if err := s.reloadCfg(); err != nil {
		return nil, bad("saved, but the config does not load: %v", err)
	}
	out := map[string]any{"fields": schemaFields(s.cfg()), "restart": true}
	if had {
		out["warning"] = commentsWarning
	}
	return out, nil
}

// putKeys writes {keys: {"commit": ["x"]}} to [keys]; a keymap conflict is a 409 with the message and nothing is written.
func (s *Server) putKeys(r *http.Request) (any, error) {
	var in struct{ Keys map[string][]string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// dry run: the same "takes the key from" check as the terminal UI's key form
	cur := s.cfg()
	old, _, _ := cur.KeymapChecked(pluginBindings())
	next := cur
	next.KeyOverrides = in.Keys
	nm, _, err := next.KeymapChecked(pluginBindings())
	if err != nil {
		return nil, &apiErr{Code: http.StatusConflict, Msg: err.Error()}
	}
	if old != nil {
		var changed []string // the actions this save edits; losing one of those is the point of the edit
		for a, specs := range in.Keys {
			if !slices.Equal(specs, cur.KeyOverrides[a]) {
				changed = append(changed, a)
			}
		}
		for a := range cur.KeyOverrides {
			if _, ok := in.Keys[a]; !ok {
				changed = append(changed, a)
			}
		}
		if len(changed) == 0 {
			changed = []string{""}
		}
		var terr error
		for _, e := range changed {
			if terr = keymap.TakenFrom(old, nm, e); terr == nil {
				break
			}
		}
		if terr != nil {
			return nil, &apiErr{Code: http.StatusConflict, Msg: terr.Error()}
		}
	}
	had := config.HasComments(s.configPath())
	if err := config.WriteKeys(s.configPath(), in.Keys, pluginBindings()...); err != nil {
		return nil, &apiErr{Code: http.StatusConflict, Msg: err.Error()}
	}
	if err := s.reloadCfg(); err != nil {
		return nil, bad("saved, but the config does not load: %v", err)
	}
	if had {
		return map[string]any{"warning": commentsWarning}, nil
	}
	return nil, nil
}

// settings returns the resolved configuration as TOML, with every secret blanked, and the file it came from.
// shortcut: read only. Editing config.toml from a browser would need a validated writer; the terminal UI opens it in $EDITOR.
func (s *Server) settings(r *http.Request) (any, error) {
	c := s.o.Cfg
	blank := func(v *string) {
		if *v != "" {
			*v = "(hidden)"
		}
	}
	noUser := func(v *string) { // a base URL may carry user:pass@
		if u, err := url.Parse(*v); err == nil && u.User != nil {
			u.User = nil
			*v = u.String()
		}
	}
	blank(&c.AI.APIKey)
	blank(&c.AI.APIKeyCmd) // a command may embed the key itself
	noUser(&c.AI.BaseURL)
	blank(&c.Web.Password)
	blank(&c.Security.Passcode) // the bcrypt hash of the lock passcode
	c.Web.TLSCert, c.Web.TLSKey, c.MCP.TLSCert, c.MCP.TLSKey = "", "", "", ""
	ps := map[string]config.AIProvider{}
	for k, p := range c.AI.Providers {
		blank(&p.APIKey)
		blank(&p.APIKeyCmd)
		noUser(&p.BaseURL)
		ps[k] = p
	}
	c.AI.Providers = ps
	c.MCP.Tokens = append([]config.MCPToken(nil), c.MCP.Tokens...)
	for i := range c.MCP.Tokens {
		t := &c.MCP.Tokens[i]
		blank(&t.Value)
		t.Name, t.Scope, t.Connections = "token", "", nil
	}
	c.Keys = nil // raw TOML primitives; the key bindings are on the help screen
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return nil, err
	}
	return map[string]any{"path": s.configPath(), "text": b.String(), "readOnly": true}, nil
}
