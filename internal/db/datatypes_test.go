package db

import (
	"slices"
	"strings"
	"testing"
)

type typesFake struct{ Session }

func (typesFake) Caps() Capabilities { return Capabilities{Dialect: "mysql"} }

type typesOwn struct{ typesFake }

func (typesOwn) Caps() Capabilities { return Capabilities{Dialect: "mysql", Types: []string{"x"}} }

func TestDataTypesOf(t *testing.T) {
	if !slices.Contains(DataTypesOf(typesFake{}), "bigint") {
		t.Error("mysql default list")
	}
	if got := DataTypesOf(typesOwn{}); len(got) != 1 || got[0] != "x" {
		t.Errorf("own list: %v", got)
	}
	for _, d := range []string{"postgres", "mysql", "clickhouse"} {
		for _, ty := range DefaultTypes(d) {
			if strings.ContainsAny(ty, `\ `) && ty != "double precision" {
				t.Errorf("%s: odd type %q", d, ty)
			}
		}
	}
	if !slices.Contains(DefaultTypes("postgres"), "double precision") {
		t.Error("double precision must be one entry")
	}
	if DataTypesOf(&mongoLike{}) != nil {
		t.Error("mongo has no types")
	}
}
