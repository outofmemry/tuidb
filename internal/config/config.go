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
	General  General                   `toml:"general"`
	Editor   Editor                    `toml:"editor"`
	Table    Table                     `toml:"table"`
	CSV      CSV                       `toml:"csv"`
	Sidebar  Sidebar                   `toml:"sidebar"`
	Theme    Theme                     `toml:"theme"`
	AI       AI                        `toml:"ai"`
	MCP      MCP                       `toml:"mcp"`
	Queries  Queries                   `toml:"queries"`
	Export   Export                    `toml:"export"`
	Web      Web                       `toml:"web"`
	Security Security                  `toml:"security"`
	Tags     []Tag                     `toml:"tag"`    // [[tag]] custom tags with colours (M4)
	Sources  []Source                  `toml:"source"` // [[source]] folders of extra connection files (M4)
	Keys     map[string]toml.Primitive `toml:"keys"`

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
	// Hints are the actions shown in the terminal status line, in order; empty = the built-in set (M13).
	Hints []string `toml:"hints"`
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
	DecodeSpatial         bool   `toml:"decode_spatial"`          // show geometry cells as WKT instead of hex (M13)
	ImagePreview          bool   `toml:"image_preview"`           // sniff image bytes and preview them in quick look (M13)
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
	Header   bool   `toml:"header"`   // CSV header row (M9); NULL text is [csv] null_as
}

// Web holds settings only the web UI reads.
type Web struct {
	TLSCert       string `toml:"tls_cert"` // serve HTTPS when both are set (M5)
	TLSKey        string `toml:"tls_key"`
	Password      string `toml:"password"`      // login page in front of the token; "" = token only (M5)
	FontSize      int    `toml:"font_size"`     // px, 0 = default (M4)
	LineHeight    string `toml:"line_height"`   // css value, "" = default (M4)
	Notifications bool   `toml:"notifications"` // desktop notifications when a backup / import finishes (M4)
	// AutoHideScrollbars shows scrollbars only while scrolling (M13).
	AutoHideScrollbars bool `toml:"auto_hide_scrollbars"`
	// Toolbar lists the action names of the web toolbar, in order; empty = the built-in set (M13).
	Toolbar []string `toml:"toolbar"`
}

// Security guards the app with a passcode (M13). Passcode is a bcrypt hash made by `webtuidb -set-passcode`.
type Security struct {
	Passcode      string `toml:"passcode"`
	LockAfterMins int    `toml:"lock_after_mins"` // idle minutes before the lock screen; 0 = never
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
		Web: Web{AutoHideScrollbars: true},
		Editor: Editor{Autocomplete: true, UppercaseKeywords: true, CloseBraces: true, CompleteKey: "enter_or_tab", Indent: "spaces", IndentWidth: 4, ResultMode: "tabs", ParamFormat: "colon", AutoSaveTabs: "first", Wrap: true, IndentType: "standard", AddSpace: true, CompleteTables: true, CompleteFunctions: true, CompleteKeywords: true, RunDefault: "current",
			AutoSaveQueries: true, HighlightCurrent: true},
		Table:   Table{AlternatingRows: true, FilterDefaultColumn: "pk", FilterDefaultOperator: "=", FilterState: "remember", DefaultSort: "none", FilterColumnSort: "ordinal", DecodeSpatial: true, ImagePreview: true},
		CSV:     CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", Decimal: ".", QuoteMode: "needed"},
		Sidebar: Sidebar{ShowRecent: true, ShowFunctions: true, ShowSequences: true, ShowTypes: true, Matching: "fuzzy", LazyLoad: true},
		Export:  Export{Filename: "{table}-{date}", Header: true},
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
	c.Theme.File = expandPathSetting(c.Theme.File) // before useInstalledTheme and the consumers stat it
	c.useInstalledTheme()
	if strings.TrimSpace(c.Export.Filename) == "" { // an empty pattern means "the default", as it always did
		c.Export.Filename = Defaults().Export.Filename
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

// expandPathSetting expands $VAR and a leading ~/ like [[source]] path does.
func expandPathSetting(p string) string {
	p = os.ExpandEnv(p)
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return p
}

// secureConfigFile chmods config.toml to 0600 when it holds a plaintext secret (web password, AI key, MCP token)
// and is readable by group/others.
func secureConfigFile(path string, c Config) error {
	secret := c.Web.Password != "" || c.AI.APIKey != "" || c.Security.Passcode != "" // the bcrypt hash of a short passcode is brute-forceable offline
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

// validate checks that the delimiter and quote are one character each and cannot be mistaken for each other
// or for a line break, so an export reads back the way it was written.
func (c CSV) validate() error {
	one := func(key, v string) (rune, error) {
		r := []rune(v)
		if len(r) != 1 || r[0] == utf8.RuneError {
			return 0, fmt.Errorf("csv.%s: %q must be exactly one character", key, v)
		}
		return r[0], nil
	}
	var d, q rune
	var err error
	if c.Delimiter != "" {
		if d, err = one("delimiter", c.Delimiter); err != nil {
			return err
		}
		if d == '\n' || d == '\r' {
			return errors.New("csv.delimiter must not be a line break")
		}
	}
	if d == 0 {
		d = ','
	}
	if c.Quote != "" {
		if q, err = one("quote", c.Quote); err != nil {
			return err
		}
		if q >= 0x7f || q == '\n' || q == '\r' || q == d {
			return fmt.Errorf("csv.quote: %q must be a printable ASCII character that is not the delimiter or a line break", c.Quote)
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
	if err := c.CSV.validate(); err != nil {
		return err
	}
	for _, e := range []struct{ key, val string }{
		{"general.new_tab_opens", c.General.NewTabOpens},
		{"editor.complete_key", c.Editor.CompleteKey},
		{"editor.indent", c.Editor.Indent},
		{"editor.result_mode", c.Editor.ResultMode},
		{"editor.param_format", c.Editor.ParamFormat},
		{"editor.auto_save_tabs", c.Editor.AutoSaveTabs},
		{"editor.indent_type", c.Editor.IndentType},
		{"editor.run_default", c.Editor.RunDefault},
		{"table.filter_column_sort", c.Table.FilterColumnSort},
		{"general.connection_sort", c.General.ConnectionSort},
		{"csv.quote_mode", c.CSV.QuoteMode},
		{"theme.name", c.Theme.Name},
		{"table.filter_default_column", c.Table.FilterDefaultColumn},
		{"table.filter_default_operator", c.Table.FilterDefaultOperator},
		{"table.filter_state", c.Table.FilterState},
		{"table.default_sort", c.Table.DefaultSort},
		{"sidebar.matching", c.Sidebar.Matching},
		{"ai.provider", c.AI.Provider},
	} {
		ok := enums[e.key]
		if e.key == "theme.name" {
			ok = append(slices.Clone(ThemeNames), InstalledThemes()...)
		}
		if !slices.Contains(ok, e.val) {
			return fmt.Errorf("%s: %q is not one of %s", e.key, e.val, strings.Join(ok, "|"))
		}
	}
	for _, l := range []struct {
		key  string
		list []string
	}{{"general.hints", c.General.Hints}, {"web.toolbar", c.Web.Toolbar}} {
		seen := map[string]bool{}
		for _, a := range l.list {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("%s: an action name must not be empty", l.key)
			}
			if !knownAction(a, l.key == "web.toolbar") {
				return fmt.Errorf("%s: %q is not an action name (see the help screen for the names)", l.key, a)
			}
			if seen[a] {
				return fmt.Errorf("%s: %q is listed twice", l.key, a)
			}
			seen[a] = true
		}
	}
	if c.General.QueryTimeoutSecs < 0 { // -5 used to save and silently turn the slow-query prompt off (T-P3-16)
		return errors.New("general.query_timeout_secs must be >= 0 (0 = no limit)")
	}
	if c.Security.LockAfterMins < 0 {
		return errors.New("security.lock_after_mins must be >= 0 (0 = never)")
	}
	if p := c.Security.Passcode; p != "" && !strings.HasPrefix(p, "$2") {
		return errors.New("security.passcode must be a bcrypt hash; make one with `webtuidb -set-passcode`")
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
	if (c.Web.TLSCert == "") != (c.Web.TLSKey == "") {
		return errors.New("web.tls_cert and web.tls_key must be set together")
	}
	if (c.MCP.TLSCert == "") != (c.MCP.TLSKey == "") {
		return errors.New("mcp.tls_cert and mcp.tls_key must be set together")
	}
	_, err := c.keymapWith(nil, true)
	return err
}

// enums are the allowed values of the enum settings (also what config.Schema offers).
var enums = map[string][]string{
	"general.new_tab_opens":         {"query", "empty"},
	"editor.complete_key":           {"enter_or_tab", "enter", "tab"},
	"editor.indent":                 {"spaces", "tabs"},
	"editor.result_mode":            {"tabs", "append"},
	"editor.param_format":           {"colon", "dollar", "at", "brace", "mustache", "question"},
	"editor.auto_save_tabs":         {"none", "first", "all"},
	"editor.indent_type":            {"standard", "tabular_left", "tabular_right"},
	"editor.run_default":            {"current", "all"},
	"table.filter_column_sort":      {"name", "ordinal"},
	"general.connection_sort":       {"name", "tag", "driver", "recent"},
	"csv.quote_mode":                {"always", "needed"},
	"table.filter_default_column":   {"pk", "any", "raw"},
	"table.filter_default_operator": {"=", "contains"},
	"table.filter_state":            {"remember", "show", "hide"},
	"table.default_sort":            {"none", "pk_asc", "pk_desc"},
	"sidebar.matching":              {"fuzzy", "contains"},
	"ai.provider":                   {"anthropic", "openai"},
}

// knownAction reports whether a hints/toolbar entry names a default action, a plugin shortcut, or (toolbar
// only) one of the web's own buttons.
func knownAction(name string, toolbar bool) bool {
	if strings.HasPrefix(name, "plugin:") || (toolbar && (name == "history" || name == "saved_queries")) {
		return true
	}
	for _, b := range keymap.DefaultBindings() {
		if b.Action == name {
			return true
		}
	}
	return false
}

// Keymap builds the prefix key map from the prefix and [keys] overrides.
func (c Config) Keymap() (*keymap.Map, error) { return c.KeymapWith(nil) }

// KeymapWith is Keymap plus extra bindings (plugin shortcuts: Action "plugin:<name>", Context keymap.Global).
// A [keys] override for a plugin that is not installed is ignored, so removing a plugin never breaks startup.
func (c Config) KeymapWith(extra []keymap.Binding) (*keymap.Map, error) {
	return c.keymapWith(extra, false)
}

// KeymapChecked is KeymapWith that skips the extra bindings that cannot be added (a clash with a default, a
// [keys] entry or another plugin; an unparsable key) and returns what it skipped, so one bad plugin never
// stops the keymap. Both UIs use it.
func (c Config) KeymapChecked(extra []keymap.Binding) (*keymap.Map, []string, error) {
	over := map[string][]string{}
	for action, specs := range c.KeyOverrides {
		if strings.HasPrefix(action, "plugin:") && !slices.ContainsFunc(extra, func(b keymap.Binding) bool { return b.Action == action }) {
			continue
		}
		over[action] = specs
	}
	return keymap.BuildChecked(c.General.Prefix, over, extra...)
}

// keymapWith builds the map. lenient (validation, which does not know the plugins) stands in a placeholder
// binding for every plugin named in [keys], so its key still takes part in conflict detection; otherwise
// overrides of plugins that are not in extra are dropped.
func (c Config) keymapWith(extra []keymap.Binding, lenient bool) (*keymap.Map, error) {
	have := map[string]bool{}
	for _, b := range extra {
		have[b.Action] = true
	}
	over := map[string][]string{}
	extra = slices.Clone(extra)
	for action, specs := range c.KeyOverrides {
		if strings.HasPrefix(action, "plugin:") && !have[action] {
			if !lenient || len(specs) == 0 {
				continue
			}
			// The placeholder's key is never bound (the override replaces it) but must parse: drop the "-n " flag,
			// and fall back to a fixed key when the spec is bad so Build reports that override by name.
			key := strings.TrimPrefix(specs[0], "-n ")
			if _, err := keymap.Parse(key); err != nil {
				key = "f12"
			}
			extra = append(extra, keymap.Binding{Action: action, Key: key, Context: keymap.Global, Desc: "plugin: " + strings.TrimPrefix(action, "plugin:")})
		}
		over[action] = specs
	}
	return keymap.Build(c.General.Prefix, over, extra...)
}
