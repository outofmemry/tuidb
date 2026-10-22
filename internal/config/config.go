// Package config loads ~/.config/tuidb/config.toml over TablePlus-like defaults.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"tuidb/internal/keymap"
	"tuidb/internal/safemode"
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
	AddSpace          bool   `toml:"add_space"`
	PrefixSchema      bool   `toml:"prefix_schema"`
	UppercaseKeywords bool   `toml:"uppercase_keywords"`
	CloseBraces       bool   `toml:"close_braces"`
	CompleteKey       string `toml:"complete_key"` // enter_or_tab | enter | tab
	Indent            string `toml:"indent"`       // spaces | tabs
	IndentWidth       int    `toml:"indent_width"`
	IndentType        string `toml:"indent_type"` // standard | tabular_left | tabular_right
	ResultMode        string `toml:"result_mode"` // tabs | append
	ReturnOnError     bool   `toml:"return_on_error"`
	AutoSaveTabs      string `toml:"auto_save_tabs"` // none | first | all
	AutoSaveQueries   bool   `toml:"auto_save_queries"`
	QueryParams       bool   `toml:"query_params"`
	ParamFormat       string `toml:"param_format"`
	ParamsInStrings   bool   `toml:"params_in_strings"`
	ShowInvisibles    bool   `toml:"show_invisibles"`
	Wrap              bool   `toml:"wrap"`
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
	ShowRecent        bool   `toml:"show_recent"`
	ShowFunctions     bool   `toml:"show_functions"`
	ShowSequences     bool   `toml:"show_sequences"`
	ShowTypes         bool   `toml:"show_types"`
	ShowColumns       bool   `toml:"show_columns"`
	LoadSystemSchemas bool   `toml:"load_system_schemas"`
	LazyLoad          bool   `toml:"lazy_load"`
	Matching          string `toml:"matching"` // fuzzy | contains
}

type Theme struct {
	Name   string            `toml:"name"` // dark | light
	Colors map[string]string `toml:"colors"`
}

func Defaults() Config {
	return Config{
		General: General{Prefix: "C-d", ReopenWorkspaces: true, NewTabOpens: "query", QueryTimeoutSecs: 300, KeepAlive: true,
			RowLimit: 300, EstimateCountOver: 1_000_000, DefaultSafeMode: "silent"},
		Editor: Editor{Autocomplete: true, AddSpace: true, UppercaseKeywords: true, CloseBraces: true, CompleteKey: "enter_or_tab",
			Indent: "spaces", IndentWidth: 4, IndentType: "standard", ResultMode: "tabs", AutoSaveTabs: "first",
			AutoSaveQueries: true, ParamFormat: "colon", Wrap: true, HighlightCurrent: true},
		Table:   Table{AlternatingRows: true, FilterDefaultColumn: "pk", FilterDefaultOperator: "=", FilterState: "remember", DefaultSort: "none"},
		CSV:     CSV{Delimiter: ",", Quote: `"`, LineBreak: "\n", Decimal: "."},
		Sidebar: Sidebar{ShowRecent: true, ShowFunctions: true, ShowSequences: true, ShowTypes: true, LazyLoad: true, Matching: "fuzzy"},
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
	_, err := c.Keymap()
	return err
}

// Keymap builds the prefix key map from the prefix and [keys] overrides.
func (c Config) Keymap() (*keymap.Map, error) { return keymap.Build(c.General.Prefix, c.KeyOverrides) }
