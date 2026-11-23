//go:build e2e

package e2e

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	core "github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

func TestProviderWorkspace(t *testing.T) {
	f := setup(t)
	proc := f.managed("--share-port", "reports=tcp:127.0.0.1:8000", "cat")
	run := proc.sessionID()
	profile := filepath.Join(f.config("a"), "device")
	conn, err := core.Connect(profile)
	check(t, err)
	defer conn.CloseWithError(0, "")
	caps, err := wire.Capabilities(conn)
	check(t, err)
	equal(t, slices.Contains(caps, wire.Workspace), true)
	rows, err := wire.Request(conn, wire.Message{Op: "workspace-supervisors"})
	check(t, err)
	row := rows.Supervisors[0]
	equal(t, row.Workspace, true)
	equal(t, row.Info.ID, run)
	equal(t, row.Info.PortAddresses["reports"], "127.0.0.1:8000")
	old, err := wire.Request(conn, wire.Message{Op: "result-supervisors"})
	check(t, err)
	equal(t, old.Supervisors[0].Workspace, false)
	equal(t, len(old.Supervisors[0].Info.PortAddresses), 0)
	sessions, err := core.List(conn)
	check(t, err)
	equal(t, len(sessions[0].PortAddresses), 0)
	call := func(op, p, version, source string, args ...string) *wire.FileEntry {
		t.Helper()
		info, err := core.Workspace(conn, wire.Message{Op: "workspace", Channel: "workspace/" + op, ID: row.ID, Path: p, Version: version, Args: args}, source, nil)
		check(t, err)
		return info
	}
	tmp := t.TempDir()
	input := filepath.Join(tmp, "input")
	local := filepath.Join(tmp, "output")
	payload := strings.Repeat("data\x00value\n", 18000)
	write(t, input, payload)
	folder := call("mkdir", "dir", "", "")
	file := call("create", "dir/data", "", input)
	equal(t, read(t, filepath.Join(f.work, "dir", "data")), payload)
	f.run("a", client, row.ID, "stop")
	copy := call("fetch", "dir/data", file.Version, local)
	equal(t, copy.ID, file.ID)
	equal(t, read(t, local), payload)
	write(t, input, "edited")
	edited := call("replace", "dir/data", file.Version, input)
	equal(t, edited.ID, file.ID)
	equal(t, edited.Version != file.Version, true)
	f.run("a", client, row.ID, "start")
	equal(t, proc.sessionID() != run, true)
	renamed := call("move", "dir/data", edited.Version, "", "dir/renamed")
	equal(t, renamed.ID, file.ID)
	var listing []wire.FileEntry
	_, err = core.Workspace(conn, wire.Message{Op: "workspace", ID: row.ID, Channel: "workspace/list", Path: "dir"}, "", func(e wire.FileEntry) error { listing = append(listing, e); return nil })
	check(t, err)
	equal(t, len(listing), 1)
	equal(t, listing[0], *renamed)
	f.run("a", client, row.ID, "fs", "cp", "local:"+input, "dir/renamed")
	_, err = core.Workspace(conn, wire.Message{Op: "workspace", Channel: "workspace/remove", ID: row.ID, Path: "dir/renamed", Version: renamed.Version}, "", nil)
	var rejection *wire.RemoteError
	if !errors.As(err, &rejection) || rejection.Kind != "conflict" {
		t.Fatalf("stale deletion: %v", err)
	}
	newFile := call("stat", "dir/renamed", "", "")
	call("remove", "dir/renamed", newFile.Version, "")
	updated := call("stat", "dir", "", "")
	equal(t, updated.ID, folder.ID)
	call("remove", "dir", updated.Version, "")
}

func TestProviderAuthorization(t *testing.T) {
	f := setup(t)
	one, onceID := f.session(nil, "cat")
	_, disabledID := f.session([]string{"--files=FALSE"}, "cat")
	dir := filepath.Join(f.config("a"), "device")
	conn, err := core.Connect(dir)
	check(t, err)
	defer conn.CloseWithError(0, "")
	rows, err := wire.Request(conn, wire.Message{Op: "workspace-supervisors"})
	check(t, err)
	for _, row := range rows.Supervisors {
		equal(t, row.Workspace, row.ID == onceID)
	}
	msg := wire.Message{Op: "workspace", ID: disabledID, Path: ".", Channel: "workspace/stat"}
	_, err = core.Workspace(conn, msg, "", nil)
	var denied *wire.RemoteError
	if !errors.As(err, &denied) || denied.Kind != "unavailable" {
		t.Fatalf("disabled workspace: %v", err)
	}
	msg.ID = onceID
	root, err := core.Workspace(conn, msg, "", nil)
	check(t, err)
	equal(t, root.Directory, true)
	// Revoke during an upload. The partial copy and its staging name must go away.
	stream, _, err := wire.Open(conn, wire.Message{Op: "workspace", ID: onceID, Path: "incomplete", Channel: "workspace/create"})
	check(t, err)
	check(t, wire.Write(stream, wire.Message{Op: "data", Data: []byte("partial")}))
	cert, err := identity.Key(dir)
	check(t, err)
	f.run("srv", daemon, "revoke", identity.Pin(cert.Leaf))
	<-conn.Context().Done()
	fresh, err := core.Connect(dir)
	if err == nil {
		defer fresh.CloseWithError(0, "")
		if _, err = core.Workspace(fresh, msg, "", nil); err == nil {
			t.Fatal("revoked enrollment retained access")
		}
	}
	eventually(t, func() bool {
		matches, err := filepath.Glob(filepath.Join(f.work, "*"))
		return err == nil && len(matches) == 0
	})
	one.stop()
	other, err := core.Connect(filepath.Join(f.config("b"), "device"))
	check(t, err)
	defer other.CloseWithError(0, "")
	_, err = core.Workspace(other, msg, "", nil)
	if !errors.As(err, &denied) || denied.Kind != "unavailable" {
		t.Fatalf("expired one-shot workspace: %v", err)
	}
}
