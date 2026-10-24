package db

import "testing"

func TestPreimage(t *testing.T) {
	got := Preimage([]Change{
		{Kind: Insert, ObjRef: ObjRef{Name: "t"}, Set: []KeyVal{{"a", 1}}},
		{Kind: Update, ObjRef: ObjRef{Name: "t"}, Key: []KeyVal{{"id", int64(7)}, {"n", "x,y"}}, Set: []KeyVal{{"n", "z"}}},
		{Kind: Delete, ObjRef: ObjRef{Name: "u"}, Key: []KeyVal{{"id", nil}}},
	})
	want := "update t,id,n\n,7,\"x,y\"\ndelete u,id\n,NULL\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
