// Package config loads ~/.config/webtuidb/config.toml over TablePlus-like defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

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
	Keys    map[string]toml.Primitive `toml:"keys"`

	KeyOverrides map[string][]string `toml:"-"` // decoded from Keys
}

type General struct {
	Prefix            string `toml:"prefix"`
	ReopenWorkspaces  bool   `toml:"reopen_workspaces"`
	NewTabOpens       string `toml:"new_tab_opens"` // query | empty
	QueryTimeoutSecs  int    `toml:"query_timeout_secs"`
	KeepAlive         bool   `toml:"keep_alive"`
	RowLimit          int    `toml:"row_limit"`
	EstimateCountOver int64  `toml:"estimate_count_over"`
	DefaultSafeMode   string `toml:"default_safe_mode"`
	LogOriginalValues bool   `toml:"log_original_values"`
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
	AutoSaveQueries   bool   `toml:"auto_save_queries"`
	HighlightCurrent  bool   `toml:"highlight_current"`
}

type Table struct {
	AlternatingRows       bool   `toml:"alternating_rows"`
	FilterDefaultColumn   string `toml:"filter_default_column"`   // pk | any | raw
	FilterDefaultOperator string `toml:"filter_default_operator"` // = | contains
	FilterState           string `toml:"filter_state"`            // remember | show | hide
	DefaultSort           string `toml:"default_sort"`            // none | pk_asc | pk_desc
}

type CSV struct {
	Delimiter string `toml:"delimiter"`
	Quote     string `toml:"quote"`
	LineBreak string `toml:"line_break"`
	Decimal   string `toml:"decimal"`
}

type Sidebar struct {
	ShowRecent    bool   `toml:"show_recent"`
	ShowFunctions bool   `toml:"show_functions"`
	ShowSequences bool   `toml:"show_sequences"`
	ShowTypes     bool   `toml:"show_types"`
	Matching      string `toml:"matching"` // fuzzy | contains
}

type Theme struct {
	Name   string            `toml:"name"` // dark | light
	Colors map[string]string `toml:"colors"`
}

func Defaults() Config {
	return Config{
		General: General{Prefix: "C-d", ReopenWorkspaces: true, NewTabOpens: "query", QueryTimeoutSecs: 300, KeepAlive: true,
			RowLimit: 300, EstimateCountOver: 1_000_000, DefaultSafeMode: "silent"},
		Editor: Editor{Autocomplete: true, UppercaseKeywords: true, CloseBraces: true, CompleteKey: "enter_or_tab", Indent: "spaces", IndentWidth: 4, ResultMode: "tabs",
			AutoSaveQueries: true, HighlightCurrent: true},
		Table:   Table{AlternatingRows: true, FilterDefaultColumn: "pk", FilterDefaultOperator: "=", FilterState: "remember", DefaultSort: "none"},
		CSV:     CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", Decimal: "."},
		Sidebar: Sidebar{ShowRecent: true, ShowFunctions: true, ShowSequences: true, ShowTypes: true, Matching: "fuzzy"},
		Theme:   Theme{Name: "dark"},
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
			if k[0] != "keys" && k[0] != "ai" && k[0] != "mcp" && k[0] != "theme" { // ai/mcp/theme.colors land in M2/M3
				return c, fmt.Errorf("%s: unknown setting %q", path, k.String())
			}
		}
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
		{"table.filter_default_column", c.Table.FilterDefaultColumn, []string{"pk", "any", "raw"}},
		{"table.filter_state", c.Table.FilterState, []string{"remember", "show", "hide"}},
		{"table.default_sort", c.Table.DefaultSort, []string{"none", "pk_asc", "pk_desc"}},
		{"sidebar.matching", c.Sidebar.Matching, []string{"fuzzy", "contains"}},
	} {
		if !slices.Contains(e.ok, e.val) {
			return fmt.Errorf("%s: %q is not one of %s", e.key, e.val, strings.Join(e.ok, "|"))
		}
	}
	// Accepted by the file format but not implemented yet: fail loudly instead of doing nothing.
	if c.General.LogOriginalValues {
		return errors.New("general.log_original_values is not implemented yet")
	}
	if c.CSV != Defaults().CSV {
		return errors.New("the [csv] settings are not implemented yet; exports use the defaults")
	}
	_, err := c.Keymap()
	return err
}

// Keymap builds the prefix key map from the prefix and [keys] overrides.
func (c Config) Keymap() (*keymap.Map, error) { return keymap.Build(c.General.Prefix, c.KeyOverrides) }
