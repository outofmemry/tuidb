package db

import (
	"reflect"
	"testing"
)

func TestParams(t *testing.T) {
	for _, c := range []struct {
		f    ParamFormat
		sql  string
		want []string
	}{
		{"colon", "select a::int, :id, :name, :id, ':no' -- :c\n/* :d */", []string{"id", "name"}},
		{"dollar", "select $1, $a, $$x$$, $b, $a", []string{"a", "b"}},
		{"at", "select @a, @@v, '@s', @b", []string{"a", "b"}},
		{"brace", "select ${a}, ${ b }, ${a b}, '${s}'", []string{"a", "b"}},
		{"mustache", "select {{a}}, {{ b }}, {{a}}", []string{"a", "b"}},
		{"question", "select ?, '?', ?", []string{"1", "2"}},
	} {
		if got := Params(c.sql, c.f, ""); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v want %v", c.f, got, c.want)
		}
	}
}

func TestBind(t *testing.T) {
	v := map[string]string{"a": "5", "b": "it's", "c": "null", "d": `x\y`, "1": "z"}
	if got, want := Bind("x=:a and y=:b and z=:c and ':a' and w=:d and q=:zz::int", "colon", v, "postgres"), `x=5 and y='it''s' and z=null and ':a' and w='x\y' and q=:zz::int`; got != want {
		t.Error(got)
	}
	if got, want := Bind("w=:d", "colon", v, "mysql"), `w='x\\y'`; got != want {
		t.Error(got)
	}
	if got, want := Bind("a=? and {{a}}", "question", v, "postgres"), "a='z' and {{a}}"; got != want {
		t.Error(got)
	}
	if got, want := Bind("a={{ a }} b=${b}", "mustache", v, "postgres"), "a=5 b=${b}"; got != want {
		t.Error(got)
	}
}

func TestParamsScanEdges(t *testing.T) {
	if got := Params(`select 'it\'s :a', :b`, "colon", "mysql"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("backslash: %v", got)
	}
	if got := Params(`select arr[i:j], a$b, user@host, :naïve`, "colon", ""); !reflect.DeepEqual(got, []string{"naïve"}) {
		t.Errorf("word: %v", got)
	}
	if got := Bind("a=:a b=:b c=:c", "colon", map[string]string{"a": "007", "b": "1234567890123456", "c": "1.5"}, ""); got != "a='007' b='1234567890123456' c=1.5" {
		t.Error(got)
	}
}

func TestBindNegative(t *testing.T) {
	if got := Bind("a -:n", "colon", map[string]string{"n": "-5"}, "postgres"); got != "a -(-5)" {
		t.Error(got)
	}
}
