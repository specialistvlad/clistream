package files

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/specialistvlad/clistream/internal/wire"
	"golang.org/x/sys/unix"
)

func TestWorkspaceConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "script")
	if err := os.WriteFile(file, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _, err := root.inspect("script")
	if err != nil {
		t.Fatal(err)
	}
	begin := func() net.Conn {
		c, s := net.Pipe()
		go func() {
			defer s.Close()
			root.ServeWorkspace(s, wire.Message{Op: "workspace", Channel: "workspace/replace", Path: "script", Version: before.Version})
		}()
		ack, err := wire.Read(c)
		if err != nil || ack.Op != "ok" {
			t.Fatalf("upload start: %v %v", ack, err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	first, second := begin(), begin()
	if err = Send(first, bytes.NewBufferString("first")); err != nil {
		t.Fatal(err)
	}
	result, err := wire.Read(first)
	if err != nil || result.Entry == nil || result.Entry.ID != before.ID {
		t.Fatalf("first save: %v %v", result, err)
	}
	if err = Send(second, bytes.NewBufferString("second")); err != nil {
		t.Fatal(err)
	}
	conflict, err := wire.Read(second)
	if err != nil || conflict.ErrorKind != "conflict" {
		t.Fatalf("second save: %v %v", conflict, err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "first" {
		t.Fatal(string(data), err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal(info, err)
	}
}

func TestWorkspaceRechecksHostDuringTransfer(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "document")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _, err := root.inspect("document")
	if err != nil {
		t.Fatal(err)
	}
	c, s := net.Pipe()
	defer c.Close()
	go func() {
		defer s.Close()
		root.ServeWorkspace(s, wire.Message{Channel: "workspace/replace", Path: "document", Version: before.Version})
	}()
	ack, err := wire.Read(c)
	if err != nil || ack.Op != "ok" {
		t.Fatal(ack, err)
	}
	if err = os.WriteFile(file, []byte("edited on host"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Send(c, bytes.NewBufferString("stale")); err != nil {
		t.Fatal(err)
	}
	result, err := wire.Read(c)
	if err != nil || result.ErrorKind != "conflict" {
		t.Fatalf("stale upload: %v %v", result, err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "edited on host" {
		t.Fatal(string(data), err)
	}
}

func TestWorkspaceRemoveEmptiedDirectory(t *testing.T) {
	base := t.TempDir()
	folder := filepath.Join(base, "folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(folder, "child")
	if err := os.WriteFile(child, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(folder, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	root, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	snapshot := func() wire.FileEntry {
		t.Helper()
		entry, _, err := root.inspect("folder")
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	remove := func(entry wire.FileEntry) error {
		_, err := root.changeWorkspace(wire.Message{Channel: "workspace/remove", Path: "folder", Version: entry.Version})
		return err
	}
	before := snapshot()
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := remove(before); !errors.Is(err, errConflict) {
		t.Fatalf("removing a stale directory: %v", err)
	}
	if err := os.WriteFile(child, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	current := snapshot()
	if current.ID != before.ID {
		t.Fatal("child changes replaced the directory's identity")
	}
	if err := remove(current); !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
		t.Fatalf("removing a nonempty directory: %v", err)
	}
	if data, err := os.ReadFile(child); err != nil || string(data) != "new" {
		t.Fatalf("new child changed: %q, %v", data, err)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := remove(snapshot()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRecursiveRemoval(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(dir, "shared")
	if err := os.MkdirAll(filepath.Join(tree, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tree, "nested", "file")
	if err := os.WriteFile(file, []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tree, "out")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	remove := func(recursive bool) error {
		item, _, err := root.inspect("shared")
		if err != nil {
			return err
		}
		_, err = root.changeWorkspace(wire.Message{Channel: "workspace/remove", Path: "shared", Version: item.Version, Recursive: recursive})
		return err
	}
	if err = remove(true); err == nil {
		t.Fatal("deleted a directory containing unexported files")
	}
	if _, err = os.Stat(file); err != nil {
		t.Fatal("partial recursive deletion", err)
	}
	if err = os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err = remove(false); err == nil {
		t.Fatal("deleted nonempty folder without recursive request")
	}
	if err = remove(true); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("unexpected staging folder", entries, err)
	}
	data, err := os.ReadFile(external)
	if err != nil || string(data) != "outside" {
		t.Fatal("outside file changed", string(data), err)
	}
}

func TestWorkspaceEntriesConfined(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(outside, []byte("off limits"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, p := range []string{"hardlink", "symlink", "../external", "/external", ".clistream-upload-private"} {
		if _, _, err = root.inspect(p); err == nil {
			t.Fatalf("exposed %s", p)
		}
	}
	blocked := wire.Message{Op: "workspace", Channel: "workspace/stat", Path: "."}
	if err = (*Root)(nil).workspace(&duplex{Reader: bytes.NewReader(nil)}, blocked); !errors.Is(err, errUnavailable) {
		t.Fatal(err)
	}
}
