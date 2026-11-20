package main

import (
	"reflect"
	"testing"
)

func TestCommandRoundTrip(t *testing.T) {
	argv := [][]byte{[]byte("worker path"), {}, []byte("界e\u0301"), []byte("$HOME;|"), []byte("tab\tline\n"), []byte("'\u0022\\"), {0xff}}
	got, err := parseCommand(formatCommand(argv))
	if err != nil || !reflect.DeepEqual(got, argv) {
		t.Fatalf("round trip got %q, %v", got, err)
	}
	got, err = parseCommand(`worker 'one two' "" ab\ c`)
	if err != nil || !reflect.DeepEqual(got, [][]byte{[]byte("worker"), []byte("one two"), {}, []byte("ab c")}) {
		t.Fatalf("quoted arguments got %q, %v", got, err)
	}
	for _, line := range []string{"", "  ", `'' arg`, `worker "`, `worker \`, `worker "\q"`} {
		if _, err := parseCommand(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestCommandCursor(t *testing.T) {
	l := commandLine{}
	l.insert("foo 界e\u0301")
	l.remove(l.adjacent(true))
	if l.text != "foo 界" {
		t.Fatal(l)
	}
	l.pos = l.word(true)
	if l.pos != len("foo ") {
		t.Fatal(l)
	}
	l.remove(l.adjacent(false))
	l.insert("again")
	if l.text != "foo again" || l.word(false) != len(l.text) {
		t.Fatal(l)
	}
}
