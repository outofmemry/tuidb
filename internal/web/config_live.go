package web

import (
	"path/filepath"
	"reflect"
	"strings"
	"webtuidb/internal/config"
)

// cfg is the configuration as the settings editor last wrote it, else the one the server started with.
// shortcut: only /api/meta and the settings routes read it; the rest of the server keeps the startup
// config (hundreds of unsynchronised reads of s.o.Cfg), so a changed setting reaches those on restart.
func (s *Server) cfg() config.Config {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if s.live != nil {
		return *s.live
	}
	return s.o.Cfg
}

func (s *Server) reloadCfg() error {
	c, err := config.Load(s.configPath())
	if err != nil {
		return err
	}
	s.liveMu.Lock()
	s.live = &c
	s.liveMu.Unlock()
	return nil
}

// configPath is the config.toml the process was started with (-config), else the default one.
func (s *Server) configPath() string {
	if s.o.CfgPath != "" {
		return s.o.CfgPath
	}
	return filepath.Join(config.ConfigDir(), "config.toml")
}

// camel turns a toml key such as filter_default_column into filterDefaultColumn.
func camel(k string) string {
	p := strings.Split(k, "_")
	for i := 1; i < len(p); i++ {
		if p[i] != "" {
			p[i] = strings.ToUpper(p[i][:1]) + p[i][1:]
		}
	}
	return strings.Join(p, "")
}

// section renders a config table with its Go field names (what the web client has always read) and, beside
// them, the camelCase spelling of every toml key (filterDefaultColumn, closeBraces, ...).
func section(v any) map[string]any {
	out := map[string]any{}
	rv := reflect.ValueOf(v)
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		out[f.Name] = rv.Field(i).Interface()
		if name := strings.Split(f.Tag.Get("toml"), ",")[0]; name != "" && name != "-" {
			out[camel(name)] = rv.Field(i).Interface()
		}
	}
	return out
}
