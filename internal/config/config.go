// Package config loads ~/.config/webtuidb/config.toml over TablePlus-like defaults.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"webtuidb/internal/keymap"
	"webtuidb/internal/safemode"
)

type Config struct {
	General General                   `toml:"general"`
	Editor  Editor                    `toml:"editor"`
	Table   Table                     `toml:"table"`
	CSV     CSV                       `toml:"csv"`
	Sidebar Sidebar                   `toml:"sidebar"`
	Theme   Theme                     `toml:"theme"`
	AI      AI                        `toml:"ai"`
	MCP     MCP                       `toml:"mcp"`
	Queries Queries                   `toml:"queries"`
	Export  Export                    `toml:"export"`
	Web     Web                       `toml:"web"`
	Tags    []Tag                     `toml:"tag"`    // [[tag]] custom tags with colours (M4)
	Sources []Source                  `toml:"source"` // [[source]] folders of extra connection files (M4)
	Keys    map[string]toml.Primitive `toml:"keys"`

	KeyOverrides map[string][]string `toml:"-"` // decoded from Keys
}

type General struct {
	Prefix             string `toml:"prefix"`
	ReopenWorkspaces   bool   `toml:"reopen_workspaces"`
	NewTabOpens        string `toml:"new_tab_opens"` // query | empty
	QueryTimeoutSecs   int    `toml:"query_timeout_secs"`
	KeepAlive          bool   `toml:"keep_alive"`
	RowLimit           int    `toml:"row_limit"`
	ShowDatabaseName   bool   `toml:"show_database_name"`   // database stack labels (M4)
	ConnectionSort     string `toml:"connection_sort"`      // name | tag | driver (M8)
	RecentFiles        int    `toml:"recent_files"`         // how many opened SQL files to remember (M6)
	ShowConnectionName bool   `toml:"show_connection_name"` // database stack labels (M4)
	EstimateCountOver  int64  `toml:"estimate_count_over"`
	DefaultSafeMode    string `toml:"default_safe_mode"`
	LogOriginalValues  bool   `toml:"log_original_values"`
}

type Editor struct {
	Autocomplete      bool   `toml:"autocomplete"`
	UppercaseKeywords bool   `toml:"uppercase_keywords"`
	CloseBraces       bool   `toml:"close_braces"`
	CompleteKey       string `toml:"complete_key"` // enter_or_tab | enter | tab
	Indent            string `toml:"indent"`       // spaces | tabs
	IndentWidth       int    `toml:"indent_width"`
	ResultMode        string `toml:"result_mode"` // tabs | append
	ReturnOnError     bool   `toml:"return_on_error"`
	QueryParams       bool   `toml:"query_params"`
	ParamFormat       string `toml:"param_format"` // colon | dollar | at | brace | mustache | question
	AutoSaveQueries   bool   `toml:"auto_save_queries"`
	AutoSaveTabs      string `toml:"auto_save_tabs"` // none | first | all: editor tabs restored on reconnect (M4)
	Wrap              bool   `toml:"wrap"`           // wrap long lines (M4)
	IndentType        string `toml:"indent_type"`    // standard | tabular_left | tabular_right (M4)
	AddSpace          bool   `toml:"add_space"`      // autocomplete appends a space (M4)
	PrefixSchema      bool   `toml:"prefix_schema"`  // autocomplete inserts schema.table (M4)
	ParamsInStrings   bool   `toml:"params_in_strings"`
	CompleteTables    bool   `toml:"complete_tables"` // autocomplete sources (M4)
	CompleteFunctions bool   `toml:"complete_functions"`
	CompleteKeywords  bool   `toml:"complete_keywords"`
	RunDefault        string `toml:"run_default"` // current | all: what the run button does (M4)
	HighlightCurrent  bool   `toml:"highlight_current"`
}

type Table struct {
	AlternatingRows       bool   `toml:"alternating_rows"`
	FilterDefaultColumn   string `toml:"filter_default_column"`   // pk | any | raw
	FilterDefaultOperator string `toml:"filter_default_operator"` // = | contains
	FilterState           string `toml:"filter_state"`            // remember | show | hide
	DefaultSort           string `toml:"default_sort"`            // none | pk_asc | pk_desc
	FilterColumnSort      string `toml:"filter_column_sort"`      // name | ordinal (M4)
	AutoPrettyJSON        bool   `toml:"auto_pretty_json"`        // pretty-print JSON cells in the grid and row detail (M8)
}

type CSV struct {
	Delimiter string `toml:"delimiter"`
	Quote     string `toml:"quote"`
	LineBreak string `toml:"line_break"`
	Decimal   string `toml:"decimal"`
	NullAs    string `toml:"null_as"`    // text written for NULL on export, read back as NULL on import (M4)
	QuoteMode string `toml:"quote_mode"` // always | needed (M6)
}

type Sidebar struct {
	ShowRecent        bool   `toml:"show_recent"`
	ShowFunctions     bool   `toml:"show_functions"`
	ShowSequences     bool   `toml:"show_sequences"`
	ShowTypes         bool   `toml:"show_types"`
	Matching          string `toml:"matching"`            // fuzzy | contains
	ShowColumns       bool   `toml:"show_columns"`        // columns under each table (M4)
	SortColumns       bool   `toml:"sort_columns"`        // alphabetically instead of ordinal (M4)
	LoadSystemSchemas bool   `toml:"load_system_schemas"` // pg_catalog, information_schema... (M4)
	LazyLoad          bool   `toml:"lazy_load"`           // objects per schema on expand (M4)
}

type Queries struct {
	SharedDirs []string `toml:"shared_dirs"` // extra saved-query folders (M4)
}

type Export struct {
	Filename string `toml:"filename"` // pattern: {connection} {database} {table} {tag} {date} {time} (M4)
}

// Web holds settings only the web UI reads.
type Web struct {
	TLSCert       string `toml:"tls_cert"` // serve HTTPS when both are set (M5)
	TLSKey        string `toml:"tls_key"`
	Password      string `toml:"password"`      // login page in front of the token; "" = token only (M5)
	FontSize      int    `toml:"font_size"`     // px, 0 = default (M4)
	LineHeight    string `toml:"line_height"`   // css value, "" = default (M4)
	Notifications bool   `toml:"notifications"` // desktop notifications when a backup / import finishes (M4)
}

type Tag struct {
	Name  string `toml:"name"`
	Color string `toml:"color"`
}

type Source struct {
	Path string `toml:"path"` // a folder of *.toml connection files, each in connections.toml format
}

type Theme struct {
	Name   string            `toml:"name"` // dark | light | auto | silver-dark | jade-dark | polaris-dark | sunset-dark | sunset-light
	File   string            `toml:"file"` // a theme TOML to import ([theme.colors] overrides on top of Name) (M4)
	Colors map[string]string `toml:"colors"`
}

// ThemeNames are the built-in palettes; "auto" follows the terminal / prefers-color-scheme.
var ThemeNames = []string{"dark", "light", "auto", "silver-dark", "jade-dark", "polaris-dark", "sunset-dark", "sunset-light"}

// AI configures the assistant. BaseURL is an OpenAI-compatible endpoint (e.g. http://localhost:11434/v1 for Ollama).
type AI struct {
	Provider  string `toml:"provider"` // anthropic | openai
	Model     string `toml:"model"`
	APIKeyCmd string `toml:"api_key_cmd"` // stdout becomes the key
	APIKey    string `toml:"api_key"`
	BaseURL   string `toml:"base_url"`
	// Providers are named alternatives for per-chat switching ([ai.providers.<name>]); the top-level fields are the default (M7).
	Providers map[string]AIProvider `toml:"providers"`
}

type AIProvider struct {
	Provider  string `toml:"provider"`
	Model     string `toml:"model"`
	APIKeyCmd string `toml:"api_key_cmd"`
	APIKey    string `toml:"api_key"`
	BaseURL   string `toml:"base_url"`
}

type MCP struct {
	Enabled           bool       `toml:"enabled"`
	Port              int        `toml:"port"`
	TLSCert           string     `toml:"tls_cert"` // HTTPS for the MCP listener (M4)
	TLSKey            string     `toml:"tls_key"`
	Listen            string     `toml:"listen"` // "" = 127.0.0.1:<port>; set an address to serve remotely (M4)
	ApprovalForUnsafe bool       `toml:"approval_for_unsafe"`
	Tokens            []MCPToken `toml:"token"`
}

type MCPToken struct {
	Name        string   `toml:"name"`
	Value       string   `toml:"value"`
	Scope       string   `toml:"scope"`       // read-only | read-write | full
	Connections []string `toml:"connections"` // empty = all
}

// ResolveAIKey returns the literal api_key, or the stdout of api_key_cmd (run without a shell).
func ResolveAIKey(a AI) (string, error) {
	if a.APIKeyCmd == "" {
		return a.APIKey, nil
	}
	argv, err := splitCommand(a.APIKeyCmd)
	if err != nil || len(argv) == 0 {
		return "", fmt.Errorf("ai.api_key_cmd: invalid command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("ai.api_key_cmd failed: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func Defaults() Config {
	return Config{
		General: General{Prefix: "C-d", ReopenWorkspaces: true, NewTabOpens: "query", QueryTimeoutSecs: 300, KeepAlive: true,
			RowLimit: 300, EstimateCountOver: 1_000_000, DefaultSafeMode: "silent", ConnectionSort: "name", RecentFiles: 10},
		Editor: Editor{Autocomplete: true, UppercaseKeywords: true, CloseBraces: true, CompleteKey: "enter_or_tab", Indent: "spaces", IndentWidth: 4, ResultMode: "tabs", ParamFormat: "colon", AutoSaveTabs: "first", Wrap: true, IndentType: "standard", AddSpace: true, CompleteTables: true, CompleteFunctions: true, CompleteKeywords: true, RunDefault: "current",
			AutoSaveQueries: true, HighlightCurrent: true},
		Table:   Table{AlternatingRows: true, FilterDefaultColumn: "pk", FilterDefaultOperator: "=", FilterState: "remember", DefaultSort: "none", FilterColumnSort: "ordinal"},
		CSV:     CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", Decimal: ".", QuoteMode: "needed"},
		Sidebar: Sidebar{ShowRecent: true, ShowFunctions: true, ShowSequences: true, ShowTypes: true, Matching: "fuzzy", LazyLoad: true},
		Export:  Export{Filename: "{table}"},
		Theme:   Theme{Name: "dark"},
		AI:      AI{Provider: "anthropic", Model: "claude-opus-5-5"},
		MCP:     MCP{Port: 51003, ApprovalForUnsafe: true},
	}
}

// Load reads path over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	c := Defaults()
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, os.ErrNotExist) {
		return c, c.validate()
	}
	if err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if len(md.Undecoded()) > 0 {
		for _, k := range md.Undecoded() {
			if k[0] != "keys" && k[0] != "theme" { // theme.colors
				return c, fmt.Errorf("%s: unknown setting %q", path, k.String())
			}
		}
	}
	if err := secureConfigFile(path, c); err != nil {
		return c, err
	}
	c.KeyOverrides = map[string][]string{}
	for action, prim := range c.Keys {
		var one string
		var many []string
		switch {
		case md.PrimitiveDecode(prim, &one) == nil:
			c.KeyOverrides[action] = []string{one}
		case md.PrimitiveDecode(prim, &many) == nil:
			c.KeyOverrides[action] = many
		default:
			return c, fmt.Errorf("%s: [keys] %s must be a string or a list of strings", path, action)
		}
	}
	return c, c.validate()
}

// secureConfigFile chmods config.toml to 0600 when it holds a plaintext secret (web password, AI key, MCP token)
// and is readable by group/others.
func secureConfigFile(path string, c Config) error {
	secret := c.Web.Password != "" || c.AI.APIKey != ""
	for _, p := range c.AI.Providers {
		secret = secret || p.APIKey != ""
	}
	for _, t := range c.MCP.Tokens {
		secret = secret || t.Value != ""
	}
	if !secret {
		return nil
	}
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("%s holds secrets and is readable by other users; chmod 600 it (%w)", path, err)
		}
	}
	return nil
}

func (c Config) validate() error {
	if _, err := safemode.Parse(c.General.DefaultSafeMode); err != nil {
		return fmt.Errorf("general.default_safe_mode: %w", err)
	}
	if c.General.RowLimit <= 0 {
		return errors.New("general.row_limit must be > 0")
	}
	for _, e := range []struct {
		key, val string
		ok       []string
	}{
		{"general.new_tab_opens", c.General.NewTabOpens, []string{"query", "empty"}},
		{"editor.complete_key", c.Editor.CompleteKey, []string{"enter_or_tab", "enter", "tab"}},
		{"editor.indent", c.Editor.Indent, []string{"spaces", "tabs"}},
		{"editor.result_mode", c.Editor.ResultMode, []string{"tabs", "append"}},
		{"editor.param_format", c.Editor.ParamFormat, []string{"colon", "dollar", "at", "brace", "mustache", "question"}},
		{"editor.auto_save_tabs", c.Editor.AutoSaveTabs, []string{"none", "first", "all"}},
		{"editor.indent_type", c.Editor.IndentType, []string{"standard", "tabular_left", "tabular_right"}},
		{"editor.run_default", c.Editor.RunDefault, []string{"current", "all"}},
		{"table.filter_column_sort", c.Table.FilterColumnSort, []string{"name", "ordinal"}},
		{"general.connection_sort", c.General.ConnectionSort, []string{"name", "tag", "driver"}},
		{"csv.quote_mode", c.CSV.QuoteMode, []string{"always", "needed"}},
		{"theme.name", c.Theme.Name, ThemeNames},
		{"table.filter_default_column", c.Table.FilterDefaultColumn, []string{"pk", "any", "raw"}},
		{"table.filter_state", c.Table.FilterState, []string{"remember", "show", "hide"}},
		{"table.default_sort", c.Table.DefaultSort, []string{"none", "pk_asc", "pk_desc"}},
		{"sidebar.matching", c.Sidebar.Matching, []string{"fuzzy", "contains"}},
		{"ai.provider", c.AI.Provider, []string{"anthropic", "openai"}},
	} {
		if !slices.Contains(e.ok, e.val) {
			return fmt.Errorf("%s: %q is not one of %s", e.key, e.val, strings.Join(e.ok, "|"))
		}
	}
	for name, p := range c.AI.Providers {
		if p.Provider != "" && !validAIProvider(p.Provider) {
			return fmt.Errorf("ai.providers.%s: provider %q is not one of anthropic|openai", name, p.Provider)
		}
	}
	seen := map[string]bool{}
	for _, t := range c.MCP.Tokens {
		if seen[t.Name] {
			return fmt.Errorf("mcp.token %q: duplicate name", t.Name)
		}
		seen[t.Name] = true
		if !slices.Contains([]string{"read-only", "read-write", "full"}, t.Scope) {
			return fmt.Errorf("mcp.token %q: scope %q is not one of read-only|read-write|full", t.Name, t.Scope)
		}
	}
	if err := ValidateTags(c.Tags); err != nil {
		return err
	}
	_, err := c.Keymap()
	return err
}

// Keymap builds the prefix key map from the prefix and [keys] overrides.
func (c Config) Keymap() (*keymap.Map, error) { return keymap.Build(c.General.Prefix, c.KeyOverrides) }
