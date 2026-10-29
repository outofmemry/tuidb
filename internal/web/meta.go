package web

import (
	"fmt"
	"webtuidb/internal/config"
	"webtuidb/internal/export"
)

// metaExtra is what the start page and the dialogs need beyond the config sections: file based drivers,
// the tags with their colours, the MCP listener, the export file-name pattern and the assistant provider.
func (s *Server) metaExtra() map[string]any {
	c := s.o.Cfg
	fb := []string{}
	for _, d := range config.Drivers {
		if config.FileBased(d) {
			fb = append(fb, d)
		}
	}
	colors := config.Tags(c.Tags)
	tags := []map[string]string{}
	for _, n := range config.DefaultTags {
		tags = append(tags, map[string]string{"name": n, "color": colors[n]})
	}
	for _, t := range c.Tags {
		if _, isDefault := indexOf(config.DefaultTags, t.Name); !isDefault {
			tags = append(tags, map[string]string{"name": t.Name, "color": t.Color})
		}
	}
	dir := ""
	if s.o.Queries != nil {
		dir = s.o.Queries.Root
	}
	return map[string]any{
		"fileBased": fb, "tags": tags, "queriesDir": dir,
		"mcp":        map[string]any{"enabled": c.MCP.Enabled, "listen": s.mcpAddr(), "tls": c.MCP.TLSCert != ""},
		"export":     map[string]any{"filename": c.Export.Filename, "header": c.Export.Header},
		"ai":         map[string]any{"provider": c.AI.Provider, "model": c.AI.Model, "providers": config.AIProviderNames(c.AI)},
		"csvWarning": export.CheckCSV(c.CSV),
	}
}

func indexOf(l []string, v string) (int, bool) {
	for i, x := range l {
		if x == v {
			return i, true
		}
	}
	return -1, false
}

// mcpAddr is where the MCP listener binds: [mcp] listen, else 127.0.0.1:port.
func (s *Server) mcpAddr() string {
	if l := s.o.Cfg.MCP.Listen; l != "" {
		return l
	}
	return fmt.Sprintf("127.0.0.1:%d", s.o.Cfg.MCP.Port)
}
