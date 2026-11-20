//go:build deployment

package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/wire"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type cluster struct {
	t                  *testing.T
	prefix, bin, image string
}

func (c cluster) invoke(input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func (c cluster) docker(args ...string) string {
	c.t.Helper()
	out, err := c.invoke("", args...)
	if err != nil {
		c.t.Fatal(err)
	}
	return out
}

func (c cluster) container(host string, flags ...string) {
	name := c.prefix + "-" + host
	args := []string{"run", "-d", "--name", name, "--hostname", host, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/state", "--tmpfs", "/tmp", "-e", "HOME=/state", "-v", c.bin + ":/opt/cli:ro"}
	args = append(args, flags...)
	c.docker(append(args, c.image, "sleep", "infinity")...)
	c.t.Cleanup(func() { c.docker("rm", "-f", name) })
}

func (c cluster) command(host string, background bool, program string, args ...string) []string {
	mode := "-i"
	if background {
		mode = "-d"
	}
	return append([]string{"exec", mode, c.prefix + "-" + host, "/opt/cli/" + program}, args...)
}

func (c cluster) wait(host string, condition func(string) bool, program string, args ...string) string {
	c.t.Helper()
	var last error
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		out, err := c.invoke("", c.command(host, false, program, args...)...)
		last = err
		if err == nil && condition(out) {
			return out
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatalf("waiting for %s on %s: %v", program, host, last)
	return ""
}

func setup(t *testing.T) cluster {
	c := cluster{t: t, prefix: fmt.Sprintf("clistream-test-%d", time.Now().UnixNano()), bin: t.TempDir(), image: os.Getenv("CLISTREAM_TEST_IMAGE")}
	if c.image == "" {
		c.image = "debian:bookworm-slim"
	}
	arch := strings.TrimSpace(c.docker("info", "--format", "{{.Architecture}}"))
	switch arch {
	case "aarch64":
		arch = "arm64"
	case "x86_64":
		arch = "amd64"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", c.bin+"/", "./cmd/...")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return c
}

func TestDeployment(t *testing.T) {
	c := setup(t)
	c.docker("network", "create", "--internal", c.prefix)
	t.Cleanup(func() { c.docker("network", "rm", c.prefix) })
	root := t.TempDir()
	sentinel := filepath.Join(root, "outside")
	if err := os.WriteFile(sentinel, []byte("host-only"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"broker", "alpha", "beta", "viewer"} {
		flags := []string{"--network", c.prefix, "--network-alias", host}
		if host == "alpha" || host == "beta" {
			dir := filepath.Join(root, host)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "origin"), []byte(host), 0600); err != nil {
				t.Fatal(err)
			}
			flags = append(flags, "-v", dir+":/workspace", "-w", "/workspace")
		}
		c.container(host, flags...)
	}
	c.docker(c.command("broker", true, "clistreamd", "--interface", "loopback", "--interface", "eth0")...)
	c.wait("broker", func(string) bool { return true }, "clistreamd", "devices")
	for _, host := range []string{"alpha", "beta", "viewer"} {
		link := strings.TrimSpace(c.docker(c.command("broker", false, "clistreamd", "--qr=false", "invite")...))
		if host == "viewer" {
			c.docker(c.command(host, false, "clistream", link)...)
		} else {
			c.docker(c.command(host, true, "clistreams", link, "/bin/sh")...)
		}
	}
	var sessions []struct {
		wire.Session
		Run string
	}
	c.wait("viewer", func(s string) bool { return json.Unmarshal([]byte(s), &sessions) == nil && len(sessions) == 2 }, "clistream", "list")
	hosts := map[string]bool{}
	ids := map[string]bool{}
	for _, item := range sessions {
		host, id := item.Host, item.ID
		hosts[host] = true
		ids[item.Run] = true
		c.docker(c.command("viewer", false, "clistream", id, "fs", "cp", "origin", "local:/tmp/"+host)...)
		if got := c.docker("exec", c.prefix+"-viewer", "cat", "/tmp/"+host); got != host {
			t.Fatal("workspace mismatch")
		}
		if _, err := c.invoke("", c.command("viewer", false, "clistream", id, "fs", "cp", "../outside", "local:/tmp/private")...); err == nil {
			t.Fatal("outside file accepted")
		}
		input := fmt.Sprintf("cat origin; for path in /workspace/../outside %q; do test ! -r \"$path\" && ! (echo changed > \"$path\") || exit 8; done; echo confined\n", sentinel)
		out, err := c.invoke(input, c.command("viewer", false, "clistream", "--send-eof", "attach", id)...)
		if err != nil || out != host+"confined\n" {
			t.Fatalf("attach: %q, %v", out, err)
		}
	}
	b, err := os.ReadFile(sentinel)
	if err != nil || string(b) != "host-only" || !hosts["alpha"] || !hosts["beta"] {
		t.Fatal("host isolation failed")
	}
	log := c.docker("exec", c.prefix+"-broker", "cat", "/state/.config/clistream/server/server.log")
	devices, connections, published, downloads := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		var e activity.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.Name == "supervisor_publish" && e.Outcome == "ok" && ids[e.Session] {
			published[e.Session] = true
			devices[e.Device] = true
			connections[e.Connection] = true
		}
		if e.Name == "channel_close" && e.Operation == "fs/get" && e.Outcome == "confirmed" && ids[e.Session] {
			downloads[e.Session] = true
		}
	}
	for _, entries := range []map[string]bool{devices, connections, published, downloads} {
		if len(entries) != 2 {
			t.Fatal("missing independent journal entries")
		}
	}
}

func TestLinuxSupervisorMacClient(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires a native macOS client")
	}
	c := setup(t)
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := socket.LocalAddr().String()
	socket.Close()
	c.container("linux", "-p", addr+":"+strings.Split(addr, ":")[1]+"/udp")
	c.docker(c.command("linux", true, "clistreamd", "--listen", ":"+strings.Split(addr, ":")[1], "--advertise", addr)...)
	c.wait("linux", func(string) bool { return true }, "clistreamd", "devices")
	link := strings.TrimSpace(c.docker(c.command("linux", false, "clistreamd", "invite")...))
	c.docker(c.command("linux", true, "clistreams", link, "cat")...)
	bin, err := filepath.Abs("../../bin/clistream")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	run := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	run("", link)
	var items []wire.Session
	for deadline := time.Now().Add(10 * time.Second); len(items) == 0 && time.Now().Before(deadline); {
		if err := json.Unmarshal([]byte(run("", "list")), &items); err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if len(items) != 1 || items[0].OS != "linux" {
		t.Fatal("Linux session missing")
	}
	id := items[0].ID
	run("", id, "stop")
	run("", id, "start")
	run("", id, "restart")
	run("", id, "edit", "--", "cat")
	if out := run("mac-to-linux\n", "--send-eof", "attach", items[0].ID); out != "mac-to-linux\n" {
		t.Fatal("cross-platform echo differs")
	}
}
