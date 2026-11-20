package main

import (
	tea "charm.land/bubbletea/v2"
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
	d := dashboardUI{page: "command", width: 40, height: 10, line: newCommandInput("codex")}
	for _, r := range " --help" {
		d.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := d.line.Value(); got != "codex --help" {
		t.Fatal(got)
	}
	d.update(tea.KeyPressMsg{Code: tea.KeyHome})
	for range 4 {
		d.update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	view := d.View()
	if view.Cursor.X != 7 || view.Cursor.Y != 4 {
		t.Fatal(view.Cursor)
	}
	d.update(tea.KeyPressMsg{Code: tea.KeyDelete})
	d.update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := d.line.Value(); got != "codex --help" {
		t.Fatal(got)
	}
	d.line = newCommandInput("界e\u0301")
	d.update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if d.line.Value() != "界" {
		t.Fatal(d.line.Value())
	}
}
