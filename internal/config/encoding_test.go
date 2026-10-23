package config

import "testing"

func TestEncodingValidation(t *testing.T) {
	c := Connection{Name: "x", Driver: Postgres, Encoding: "UTF8"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Encoding = "utf8; drop"
	if err := c.Validate(); err == nil {
		t.Fatal("bad encoding accepted")
	}
}
