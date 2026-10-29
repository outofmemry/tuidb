package ui

import (
	"strings"
	"testing"
)

func TestM8AutoPrettyJSON(t *testing.T) {
	old := autoPrettyJSON
	defer func() { autoPrettyJSON = old }()
	raw := `{"a":1,"b":[1,2],"c":"x,y:z"}`
	if got, ok := spacedJSON(raw); !ok || got != `{"a": 1, "b": [1, 2], "c": "x,y:z"}` {
		t.Fatalf("spaced %q", got)
	}
	if _, ok := spacedJSON("plain"); ok {
		t.Fatal("not JSON")
	}
	autoPrettyJSON = false
	if gridCellText(raw, 60) != raw {
		t.Fatal("off: unchanged")
	}
	autoPrettyJSON = true
	if got := gridCellText(raw, 60); got != `{"a": 1, "b": [1, 2], "c": "x,y:z"}` {
		t.Fatalf("wide enough: %q", got)
	}
	if got := gridCellText(raw, 10); got != raw {
		t.Fatalf("too narrow stays compact: %q", got)
	}
	if got := detailText(raw); !strings.Contains(got, "\n  \"a\": 1") {
		t.Fatalf("row detail is indented: %q", got)
	}
}
