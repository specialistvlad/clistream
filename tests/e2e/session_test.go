//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCLIOptions(t *testing.T) {
	f := setup(t)
	for _, bin := range []string{daemon, supervisor, client} {
		help := f.start("a", bin, "--help")
		help.expect(0)
		usage := help.diagnostics.contents()
		for _, option := range []string{"--state", "--driver", "--drivers", "-state", "-unknown"} {
			excludes(t, usage, option)
			contains(t, f.reject("a", bin, option), "unknown option")
		}
		short := f.start("a", bin, "-h")
		short.expect(0)
		equal(t, short.diagnostics.contents(), usage)
	}
	for _, c := range [][2]string{{daemon, "-interface"}, {supervisor, "-interactive"}, {client, "-eof"}} {
		contains(t, f.reject("a", c[0], c[1]), "require --")
	}
	contains(t, f.reject("sup", supervisor, "--mode", "wrong", "true"), "--mode")
}

func TestSharedSession(t *testing.T) {
	f := setup(t)
	p, id := f.session(nil, "/bin/sh", "-c", `echo ready; while read -r word; do echo "out:$word"; echo "err:$word" >&2; done; exit 7`)
	a := f.start("a", client, "attach", id)
	b := f.start("b", client, "attach", id)
	a.see("ready\n")
	b.see("ready\n")
	for i, source := range []*process{a, b, p} {
		word := []string{"first", "second", "local"}[i]
		source.send(word + "\n")
		for _, v := range []*process{a, b} {
			v.see("out:" + word + "\n")
			v.diagnostics.until(t, "err:"+word+"\n")
		}
	}
	check(t, p.input.Close())
	a.expect(7)
	b.expect(7)
	p.expect(7)
	f.server.stop()
	counts := map[string]int{}
	for _, e := range records(t, read(t, f.journal())) {
		if e.Outcome == "exited" {
			equal(t, e.Exit, 7)
			counts[e.Operation]++
		}
	}
	equal(t, counts["stdout"], 2)
	equal(t, counts["stderr"], 2)
}

func TestProgramArguments(t *testing.T) {
	f := setup(t)
	contains(t, f.reject("fresh", supervisor, "true"), "clistreamd invite")
	link := strings.TrimSpace(f.run("srv", daemon, "supervisor"))
	f.reject("fresh", supervisor, link)
	f.reject("fresh", supervisor, "--once", link, "/missing-program")
	p := f.start("fresh", supervisor, "--once", "--interactive", link, "--", "/bin/sh", "-c", `test ! -t 0 || exit 8; echo "$1:$2"; read x; echo "reply-$x"; echo error >&2; exit 7`, "sh", "-child", "--help")
	v := f.start("a", client, "attach", p.sessionID())
	v.see("-child:--help\n")
	v.send("go\n")
	v.see("reply-go\n")
	v.diagnostics.until(t, "error\n")
	v.expect(7)
	p.expect(7)
	for _, name := range []string{"claude", "codex"} {
		path := filepath.Join(f.work, name)
		write(t, path, "#!/bin/sh\nprintf '%s\\n' \"$@\" >> calls\nexec cat\n")
		check(t, os.Chmod(path, 0700))
		child, id := f.session(nil, path, "--child")
		data := f.run("a", client, "list")
		excludes(t, data, "\"Driver\"")
		excludes(t, data, "\"Version\"")
		equal(t, f.sessions("b")[0].Mode, "pipe")
		f.run("a", client, "--eof", "attach", id)
		child.expect(0)
	}
	equal(t, read(t, filepath.Join(f.work, "calls")), "--child\n--child\n")
}

func TestSharedDefaultIdentity(t *testing.T) {
	f := setup(t)
	link := strings.TrimSpace(f.run("srv", daemon, "invite"))
	for _, first := range []string{"client", "supervisor", "concurrent"} {
		before := strings.Fields(f.run("srv", daemon, "devices"))
		var jobs []*process
		if first != "supervisor" {
			jobs = append(jobs, f.start(first, client, link))
		}
		if first != "client" {
			jobs = append(jobs, f.start(first, supervisor, "--once", link, "true"))
		}
		for _, p := range jobs {
			p.expect(0)
		}
		f.run(first, client, "list")
		f.run(first, supervisor, "--once", "true")
		equal(t, len(strings.Fields(f.run("srv", daemon, "devices")))-len(before), 2)
	}
	f.reject("unpaired", client, "list")
	for _, c := range []struct{ program, option string }{{client, "pair"}, {supervisor, "--pair"}} {
		for range 2 {
			p := f.start("stdin-"+c.program, c.program, c.option)
			p.send(link + "\n")
			p.input.Close()
			p.expect(0)
		}
		f.run("stdin-"+c.program, client, "list")
	}
}

func TestLegacyProfiles(t *testing.T) {
	f := setup(t)
	link := strings.TrimSpace(f.run("srv", daemon, "invite"))
	for _, roles := range [][]string{{"client"}, {"supervisor"}, {"client", "supervisor"}} {
		account := strings.Join(roles, "-")
		root := f.config(account)
		check(t, os.MkdirAll(root, 0700))
		var lastKey string
		for _, role := range roles {
			before := strings.Fields(f.run("srv", daemon, "devices"))
			f.run("import", client, link)
			for _, key := range strings.Fields(f.run("srv", daemon, "devices")) {
				if !slices.Contains(before, key) {
					lastKey = key
				}
			}

			check(t, os.Rename(filepath.Join(f.config("import"), "device"), filepath.Join(root, role)))
		}
		f.run(account, client, "list")
		f.run(account, supervisor, "--once", "true")
		if len(roles) == 2 {
			supervisorKey := read(t, filepath.Join(root, "supervisor", "key.pem"))
			equal(t, read(t, filepath.Join(root, "client", "key.pem")) == supervisorKey, false)
			f.server.stop()
			path := filepath.Join(f.config("srv"), "server", "devices.json")
			var devices map[string]string
			check(t, json.Unmarshal([]byte(read(t, path)), &devices))
			for key := range devices {
				devices[key] = "client"
			}
			devices[lastKey] = "supervisor"
			encoded, err := json.Marshal(devices)
			check(t, err)
			write(t, path, string(encoded))
			f.startServer()
			f.run("srv", daemon, "revoke", lastKey)
			f.run(account, client, "list")
			f.run(account, supervisor, "--once", "true")
			equal(t, read(t, filepath.Join(root, "supervisor", "key.pem")) == supervisorKey, true)
		}
	}
}

func TestConflictingDefaultServers(t *testing.T) {
	f := setup(t)
	other := f.start("other", daemon, "--listen", "127.0.0.1:0")
	other.diagnostics.until(t, "serving QUIC")
	root := f.config("conflict")
	check(t, os.MkdirAll(root, 0700))
	for _, r := range []struct{ server, role string }{{"srv", "client"}, {"other", "supervisor"}} {
		f.run("import", client, strings.TrimSpace(f.run(r.server, daemon, "invite")))
		check(t, os.Rename(filepath.Join(f.config("import"), "device"), filepath.Join(root, r.role)))
	}
	p := f.start("conflict", supervisor, "--once", "--interactive", "cat")
	id := p.sessionID()
	equal(t, len(f.sessions("conflict")), 0)
	link := strings.TrimSpace(f.run("other", daemon, "invite"))
	f.run("conflict", client, link)
	equal(t, f.sessions("conflict")[0].ID, id)
	equal(t, len(strings.Split(strings.TrimSpace(f.run("other", daemon, "devices")), "\n")), 1)
}

func TestActiveRevocation(t *testing.T) {
	f := setup(t)
	_, id := f.session(nil, "sh", "-c", "echo ready; exec cat")
	before := strings.Fields(f.run("srv", daemon, "devices"))
	link := strings.TrimSpace(f.run("srv", daemon, "invite"))
	f.run("guest", client, link)
	var key string
	for _, v := range strings.Fields(f.run("srv", daemon, "devices")) {
		if !slices.Contains(before, v) {
			key = v
		}
	}
	guest := f.start("guest", client, "attach", id)
	retained := f.start("a", client, "attach", id)
	guest.see("ready\n")
	retained.see("ready\n")
	f.run("srv", daemon, "revoke", key)
	if guest.wait() == 0 {
		t.Fatal("revoked attachment succeeded")
	}
	f.reject("guest", client, "list")
	contains(t, f.reject("guest", client), "no longer authorized")
	f.run("guest", client, link)
	f.run("guest", client, "list")
	f.echo(retained, "still-enrolled")
	f.server.stop()
	contains(t, read(t, f.journal()), `"outcome":"revoked"`)
}

func TestAuthorization(t *testing.T) {
	f := setup(t)
	dir := filepath.Join(f.config("unknown"), "device")
	check(t, os.MkdirAll(dir, 0700))
	write(t, filepath.Join(dir, "server.json"), read(t, filepath.Join(f.config("a"), "device", "server.json")))
	f.reject("unknown", client, "list")
	f.profile("unknown", "Pin", strings.Repeat("0", 64))
	contains(t, f.reject("unknown", client, "list"), "mismatch")
	contains(t, f.reject("unknown", client), "mismatch")
	f.run("sup", client, "list")
	f.server.stop()
	f.startServer()
	f.run("a", client, "list")
	for line := range strings.SplitSeq(strings.TrimSpace(f.run("srv", daemon, "devices")), "\n") {
		f.run("srv", daemon, "revoke", strings.Fields(line)[0])
	}
	f.reject("a", client, "list")
}

func TestLargeBinaryStdio(t *testing.T) {
	f := setup(t)
	local, id := f.session([]string{"--local-input=false"}, "cat")
	v := f.start("a", client, "--eof", "attach", id)
	data := make([]byte, 4<<20)
	rand.Read(data)
	v.send(string(data))
	check(t, v.input.Close())
	v.expect(0)
	local.expect(0)
	if v.out.contents() != string(data) || local.out.contents() != string(data) {
		t.Fatal("binary output differs")
	}
	equal(t, v.diagnostics.contents(), "")
}

func TestFiles(t *testing.T) {
	f := setup(t)
	_, id := f.session(nil, "cat")
	v := f.start("b", client, "attach", id)
	f.echo(v, "before")
	data := make([]byte, 180015)
	rand.Read(data)
	source := filepath.Join(f.base, "source")
	write(t, source, string(data))
	target := filepath.Join(f.base, "copy")
	check(t, os.Mkdir(filepath.Join(f.work, "folder"), 0700))
	check(t, os.Symlink(f.base, filepath.Join(f.work, "escape")))
	check(t, os.Link(source, filepath.Join(f.work, "hardlink")))
	f.run("a", client, "put", id, source, "folder/file")
	f.run("b", client, "get", id, "folder/file", target)
	if read(t, target) != string(data) {
		t.Fatal("transfer differs")
	}
	write(t, filepath.Join(f.work, "folder", "local-change"), "")
	names := map[string]bool{}
	for row := range strings.SplitSeq(strings.TrimSpace(f.run("a", client, "ls", id, "folder")), "\n") {
		var v struct{ Name string }
		check(t, json.Unmarshal([]byte(row), &v))
		names[v.Name] = true
	}
	equal(t, len(names), 2)
	equal(t, names["local-change"] && names["file"], true)
	f.echo(v, "after")
	for _, name := range []string{"../source", "/etc/passwd", "escape/source", "hardlink"} {
		f.reject("a", client, "get", id, name, target+"2")
	}
	for _, name := range []string{"../outside", "escape/outside", "folder/file"} {
		f.reject("a", client, "put", id, source, name)
	}
	if _, err := os.Stat(filepath.Join(f.base, "outside")); !os.IsNotExist(err) {
		t.Fatal("unexpected outside file")
	}
}

func TestConcurrentUploads(t *testing.T) {
	f := setup(t)
	_, id := f.session(nil, "cat")
	var workers []*process
	var payloads []string
	for _, account := range []string{"a", "b"} {
		data := make([]byte, 256001)
		rand.Read(data)
		path := filepath.Join(f.base, account+".bin")
		write(t, path, string(data))
		payloads = append(payloads, string(data))
		workers = append(workers, f.start(account, client, "put", id, path, "winner"))
	}
	winners := 0
	for i, p := range workers {
		if p.wait() == 0 {
			winners++
			if read(t, filepath.Join(f.work, "winner")) != payloads[i] {
				t.Fatal("winning upload differs")
			}
		}
	}
	equal(t, winners, 1)
	tmp, err := filepath.Glob(filepath.Join(f.work, ".clistream-upload-*"))
	check(t, err)
	equal(t, len(tmp), 0)
}

func TestReconnect(t *testing.T) {
	f := setup(t)
	p, old := f.session(nil, "cat")
	f.echo(p, "before-restart")
	f.server.stop()
	f.startServer()
	var id string
	eventually(t, func() bool {
		items := f.sessions("a")
		if len(items) == 0 {
			return false
		}
		id = items[0].ID
		return true
	})
	if id == old {
		t.Fatal("single-run registration reused ID")
	}
	v := f.start("b", client, "attach", id)
	v.see("before-restart\n")
	f.server.stop()
	var events []event
	for _, e := range records(t, read(t, f.journal())) {
		if e.Event == "session_register" && e.Outcome == "ok" {
			events = append(events, e)
		}
	}
	equal(t, len(events), 2)
	equal(t, events[0].Device, events[1].Device)
	equal(t, events[0].Connection == events[1].Connection, false)
}

func TestKeystrokeLatency(t *testing.T) {
	f := setup(t)
	_, id := f.session([]string{"--mode", "pty"}, "sh", "-c", "stty raw -echo; echo ready; exec cat")
	v := f.start("a", client, "attach", id)
	v.see("ready\n")
	var samples []time.Duration
	for range 21 {
		start := time.Now()
		v.send("x")
		v.see("x")
		samples = append(samples, time.Since(start))
	}
	slices.Sort(samples)
	t.Log("median echo including observation delay", samples[len(samples)/2])
}
