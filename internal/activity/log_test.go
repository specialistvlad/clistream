package activity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func records(t *testing.T, data []byte) []Event {
	t.Helper()
	var events []Event
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var event Event
		if err := json.Unmarshal(line, &event); err != nil || event.Time.IsZero() || event.Time.Location().String() != "UTC" {
			t.Fatalf("invalid record: %s (%v)", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestAppendAndRotate(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	l := Open(dir, false, &stderr)
	l.Record(Event{Name: "initial"})
	l.Close()
	l = Open(dir, false, &stderr)
	defer l.Close()
	var writers sync.WaitGroup
	for range 40 {
		writers.Go(func() { l.Record(Event{Name: "concurrent"}) })
	}
	writers.Wait()
	read := func(path string) []Event {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("log permissions: %v (%v)", info, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return records(t, data)
	}
	events := read(l.path)
	if len(events) != 41 || events[0].Name != "initial" {
		t.Fatalf("append lost records: %d", len(events))
	}
	l.limit = 1
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		l.Record(Event{Name: name})
	}
	names := []string{"g", "f", "e", "d", "c", "b"}
	paths, err := filepath.Glob(l.path + "*")
	if err != nil || len(paths) != len(names) {
		t.Fatalf("rotation: %v %v", paths, err)
	}
	for i, path := range paths {
		if got := read(path); len(got) != 1 || got[0].Name != names[i] {
			t.Fatalf("unexpected rotation at %s", path)
		}
	}
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
}

func TestLogFallback(t *testing.T) {
	for _, mode := range []string{"open", "write", "rotate"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "server.log")
			if mode == "open" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			var stderr bytes.Buffer
			l := Open(dir, false, &stderr)
			defer l.Close()
			switch mode {
			case "write":
				l.file.Close()
			case "rotate":
				l.Record(Event{Name: "before"})
				l.limit = 1
				if err := os.WriteFile(path+".4", nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path+".5", 0700); err != nil {
					t.Fatal(err)
				}
			}
			l.Record(Event{Name: "first"})
			l.Record(Event{Name: "second"})
			diagnostic, data, found := strings.Cut(stderr.String(), "\n")
			if !found || !strings.Contains(diagnostic, "writing events to stderr") {
				t.Fatal("missing fallback diagnostic")
			}
			got := records(t, []byte(data))
			if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
				t.Fatal("lost fallback records")
			}
		})
	}
}
