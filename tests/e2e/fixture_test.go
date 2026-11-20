//go:build e2e || compatibility

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/specialistvlad/clistream/internal/wire"
)

const (
	daemon     = "clistreamd"
	supervisor = "clistream-supervise"
	client     = "clistream"
)

type capture struct {
	terminal bool
	mu       sync.Mutex
	data     bytes.Buffer
	offset   int
	changed  chan struct{}
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed != nil {
		close(c.changed)
		c.changed = nil
	}
	return c.data.Write(p)
}

func (c *capture) contents() string { c.mu.Lock(); defer c.mu.Unlock(); return c.data.String() }

func (c *capture) until(t *testing.T, marker string) string {
	t.Helper()
	var visible *regexp.Regexp
	if c.terminal {
		// A renderer may toggle the cursor between two chunks of visible text.
		chars := strings.Split(marker, "")
		for i, ch := range chars {
			chars[i] = regexp.QuoteMeta(ch)
		}
		visible = regexp.MustCompile(strings.Join(chars, `(?:\x1b\[\?25[hl])*`))
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		c.mu.Lock()
		s := c.data.Bytes()[c.offset:]
		i := bytes.Index(s, []byte(marker))
		end := -1
		if i >= 0 {
			end = i + len(marker)
		} else if visible != nil {
			if match := visible.FindIndex(s); match != nil {
				end = match[1]
			}
		}
		if end >= 0 {
			result := string(s[:end])
			c.offset += end
			c.mu.Unlock()
			return result
		}
		if time.Now().After(deadline) {
			tail := string(s[max(0, len(s)-512):])
			c.mu.Unlock()
			t.Fatalf("waiting for %q, last output %q", marker, tail)
		}
		if c.changed == nil {
			c.changed = make(chan struct{})
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-time.After(time.Until(deadline)):
		}
	}
}

type process struct {
	t                *testing.T
	cmd              *exec.Cmd
	input            io.WriteCloser
	out, diagnostics capture
	done             chan struct{}
	tty              *os.File
}

func (p *process) send(s string) { p.t.Helper(); _, err := io.WriteString(p.input, s); check(p.t, err) }

func (p *process) wait() int {
	p.t.Helper()
	select {
	case <-p.done:
		return p.cmd.ProcessState.ExitCode()
	case <-time.After(40 * time.Second):
		p.t.Fatalf("process %s timed out: %s", p.cmd.Path, p.diagnostics.contents())
		return -1
	}
}

func (p *process) expect(status int) {
	p.t.Helper()
	if got := p.wait(); got != status {
		p.t.Fatalf("%s exit %d, expected %d: %s", p.cmd.Path, got, status, p.diagnostics.contents())
	}
}

func (p *process) stop() { _ = p.cmd.Process.Signal(syscall.SIGTERM); p.wait() }

func (p *process) resize(cols, rows uint16) {
	check(p.t, pty.Setsize(p.tty, &pty.Winsize{Cols: cols, Rows: rows}))
	check(p.t, p.cmd.Process.Signal(syscall.SIGWINCH))
}

func (p *process) see(s string) string { p.t.Helper(); return p.out.until(p.t, s) }

func (p *process) sessionID() string {
	p.t.Helper()
	p.diagnostics.until(p.t, "session ")
	return strings.Fields(p.diagnostics.until(p.t, "\n"))[0]
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func equal[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		if condition() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	check(t, err)
	return string(b)
}

func write(t *testing.T, path, data string) {
	t.Helper()
	check(t, os.WriteFile(path, []byte(data), 0600))
}

type fixture struct {
	t                        *testing.T
	base, work, bin, address string
	server                   *process
}

func setup(t *testing.T) *fixture {
	t.Helper()
	// Darwin limits the entire Unix socket pathname to 104 bytes.
	base, err := os.MkdirTemp("/tmp", "cs-")
	check(t, err)
	t.Cleanup(func() { os.RemoveAll(base) })
	bin := os.Getenv("CLISTREAM_BIN")
	if bin == "" {
		bin = "../../bin"
	}
	bin, err = filepath.Abs(bin)
	check(t, err)
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	check(t, err)
	addr := udp.LocalAddr().String()
	check(t, udp.Close())
	f := &fixture{t: t, base: base, work: filepath.Join(base, "workspace"), bin: bin, address: addr}
	check(t, os.Mkdir(f.work, 0700))
	f.startServer()
	link := strings.TrimSpace(f.run("srv", daemon, "invite"))
	for _, account := range []string{"sup", "a", "b"} {
		f.run(account, client, link)
	}
	return f
}

func (f *fixture) startServer(flags ...string) {
	args := append([]string{"--qr=false", "--listen", f.address}, flags...)
	f.server = f.start("srv", daemon, args...)
	f.server.diagnostics.until(f.t, "serving QUIC")
}

func (f *fixture) config(account string) string {
	dir := filepath.Join(f.base, account, "config")
	if runtime.GOOS == "darwin" {
		dir = filepath.Join(f.base, account, "Library", "Application Support")
	}
	return filepath.Join(dir, "clistream")
}

func (f *fixture) command(account, program string, args ...string) *exec.Cmd {
	f.t.Helper()
	path := program
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.bin, program)
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = f.work
	home := filepath.Join(f.base, account)
	check(f.t, os.MkdirAll(home, 0700))
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "TERM=xterm-256color")
	return cmd
}

func (f *fixture) launch(terminal bool, account, program string, args ...string) *process {
	return f.spawn(f.command(account, program, args...), terminal)
}

func (f *fixture) spawn(cmd *exec.Cmd, terminal bool) *process {
	p := &process{t: f.t, cmd: cmd, done: make(chan struct{})}
	if terminal {
		p.out.terminal = true
		tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 24})
		check(f.t, err)
		p.input = tty
		p.tty = tty
		go func() { io.Copy(&p.out, tty) }()
	} else {
		cmd.Stdout = &p.out
		cmd.Stderr = &p.diagnostics
		if cmd.Stdin == nil {
			stdin, err := cmd.StdinPipe()
			check(f.t, err)
			p.input = stdin
		}
		check(f.t, cmd.Start())
	}
	go func() { cmd.Wait(); close(p.done) }()
	f.t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = cmd.Process.Signal(syscall.SIGCONT)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-p.done
			}
		}
		if p.input != nil {
			p.input.Close()
		}
	})
	return p
}

func (f *fixture) start(account, program string, args ...string) *process {
	f.t.Helper()
	return f.launch(false, account, program, args...)
}

func (f *fixture) ui(account, program string, args ...string) *process {
	f.t.Helper()
	return f.launch(true, account, program, args...)
}

func (f *fixture) run(account, program string, args ...string) string {
	f.t.Helper()
	p := f.start(account, program, args...)
	p.input.Close()
	p.expect(0)
	return p.out.contents()
}

func (f *fixture) reject(account, program string, args ...string) string {
	f.t.Helper()
	p := f.start(account, program, args...)
	p.input.Close()
	if p.wait() == 0 {
		f.t.Fatalf("unexpected success: %s", program)
	}
	return p.diagnostics.contents()
}

func (f *fixture) sessions(account string) []wire.Session {
	f.t.Helper()
	var items []wire.Session
	check(f.t, json.Unmarshal([]byte(f.run(account, client, "list")), &items))
	return items
}

func (f *fixture) session(flags []string, cmd ...string) (*process, string) {
	f.t.Helper()
	args := append([]string{"--interactive", "--once"}, flags...)
	p := f.start("sup", supervisor, append(append(args, "--"), cmd...)...)
	return p, p.sessionID()
}

func (f *fixture) managed(cmd ...string) *process {
	f.t.Helper()
	return f.start("sup", supervisor, append([]string{"--interactive", "--local-input=false"}, cmd...)...)
}

func (f *fixture) journal() string { return filepath.Join(f.config("srv"), "server", "server.log") }

type event struct {
	Event, Operation, Outcome, Device, Role string
	Session                                 string `json:"session_id"`
	Supervisor                              string `json:"supervisor_id"`
	Connection                              string `json:"connection_id"`
	Stream                                  int64  `json:"stream_id"`
	Exit                                    int    `json:"exit_code"`
}

func records(t *testing.T, data string) []event {
	t.Helper()
	var result []event
	for row := range strings.SplitSeq(data, "\n") {
		if strings.HasPrefix(row, "{") {
			var e event
			check(t, json.Unmarshal([]byte(row), &e))
			result = append(result, e)
		}
	}
	return result
}

func contains(t *testing.T, s, part string) {
	t.Helper()
	if !strings.Contains(s, part) {
		t.Fatalf("missing %q", part)
	}
}

func excludes(t *testing.T, s, part string) {
	t.Helper()
	if strings.Contains(s, part) {
		t.Fatalf("unexpected %q", part)
	}
}

func (f *fixture) logs() []string {
	paths, err := filepath.Glob(filepath.Join(f.config("sup"), "device", "logs", "*.log"))
	check(f.t, err)
	return paths
}

func (f *fixture) profile(account, field, value string) {
	path := filepath.Join(f.config(account), "device", "server.json")
	var data map[string]any
	check(f.t, json.Unmarshal([]byte(read(f.t, path)), &data))
	data[field] = value
	b, err := json.Marshal(data)
	check(f.t, err)
	write(f.t, path, string(b))
}

func (f *fixture) portForward(id, name string) *process {
	return f.start("a", client, "forward", id, name, "127.0.0.1:0")
}

func (p *process) forwardAddress() string {
	p.diagnostics.until(p.t, "forwarding ")
	fields := strings.Fields(p.diagnostics.until(p.t, "\n"))
	return fields[len(fields)-1]
}

func (p *process) restart() { p.send("r"); p.see("Enter Restart & attach"); p.send("\r") }

func (f *fixture) echo(p *process, word string) { p.send(word + "\n"); p.see(word + "\n") }
