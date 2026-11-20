//go:build e2e

package e2e

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPorts(t *testing.T) {
	f := setup(t)
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	check(t, err)
	defer udp.Close()
	go func() {
		conn, err := tcp.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		data, _ := io.ReadAll(conn)
		conn.Write(data)
	}()
	go func() {
		var b [65535]byte
		for {
			n, addr, err := udp.ReadFrom(b[:])
			if err != nil {
				return
			}
			udp.WriteTo(b[:n], addr)
		}
	}()
	_, id := f.session([]string{"--share-port", "private-tcp=tcp:" + tcp.Addr().String(), "--share-port=private-udp=udp:" + udp.LocalAddr().String()}, "cat")
	f.reject("a", client, "forward", id, "undeclared", "127.0.0.1:0")
	for _, proto := range []string{"tcp", "udp"} {
		p := f.portForward(id, "private-"+proto)
		c, err := net.Dial(proto, p.forwardAddress())
		check(t, err)
		defer c.Close()
		check(t, c.SetDeadline(time.Now().Add(8*time.Second)))
		if proto == "tcp" {
			_, err = c.Write([]byte("data"))
			check(t, err)
			check(t, c.(*net.TCPConn).CloseWrite())
			data, err := io.ReadAll(c)
			check(t, err)
			equal(t, string(data), "data")
		} else {
			for _, size := range []int{0, 1, 1400} {
				b := make([]byte, size)
				rand.Read(b)
				_, err = c.Write(b)
				check(t, err)
				recv := make([]byte, 65535)
				n, err := c.Read(recv)
				check(t, err)
				equal(t, string(recv[:n]), string(b))
			}
		}
	}
	f.server.stop()
	data := read(t, f.journal())
	excludes(t, data, "private-")
	ops := map[string]bool{}
	for _, e := range records(t, data) {
		if e.Event == "channel_close" {
			ops[e.Operation] = true
		}
	}
	equal(t, len(ops), 2)
	equal(t, ops["port/tcp"] && ops["port/udp"], true)
}

func TestRunBoundChannels(t *testing.T) {
	f := setup(t)
	service, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	defer service.Close()
	p := f.managed("--share-port", "test=tcp:"+service.Addr().String(), "cat")
	old := p.sessionID()
	forward := f.portForward(old, "test")
	local, err := net.DialTimeout("tcp", forward.forwardAddress(), 5*time.Second)
	check(t, err)
	defer local.Close()
	local.SetDeadline(time.Now().Add(15 * time.Second))
	io.WriteString(local, "old")
	service.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	remote, err := service.Accept()
	check(t, err)
	defer remote.Close()
	remote.SetDeadline(time.Now().Add(15 * time.Second))
	b := make([]byte, 3)
	_, err = io.ReadFull(remote, b)
	check(t, err)
	equal(t, string(b), "old")
	ui := f.ui("a", client)
	ui.see("R Restart")
	ui.restart()
	next := p.sessionID()
	equal(t, next == old, false)
	for _, c := range []net.Conn{remote, local} {
		_, err = c.Read(b)
		equal(t, err, io.EOF)
	}
	f.reject("a", client, "ls", old)
	f.run("a", client, "ls", next)
}

// relay introduces bounded delay, deterministic loss and a change of upstream source port.
type relay struct {
	front               *net.UDPConn
	upstream            *net.UDPConn
	server, peer, first *net.UDPAddr
	mu                  sync.Mutex
	count               int
	dropFirst           bool
	dropped             atomic.Int64
}

func udpSocket(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	check(t, err)
	return c
}

func newRelay(f *fixture, dropFirst bool) *relay {
	dest, err := net.ResolveUDPAddr("udp", f.address)
	check(f.t, err)
	r := &relay{front: udpSocket(f.t), server: dest, dropFirst: dropFirst}
	r.rebind(f.t)
	go r.pump(r.front, true)
	f.t.Cleanup(func() { r.front.Close(); r.mu.Lock(); r.upstream.Close(); r.mu.Unlock() })
	f.profile("a", "Address", r.front.LocalAddr().String())
	return r
}

func (r *relay) rebind(t *testing.T) {
	c := udpSocket(t)
	r.mu.Lock()
	if r.upstream != nil {
		r.upstream.Close()
	}
	r.upstream = c
	r.mu.Unlock()
	go r.pump(c, false)
}

func (r *relay) pump(c *net.UDPConn, fromClient bool) {
	var buf [65535]byte
	for {
		n, addr, err := c.ReadFromUDP(buf[:])
		if err != nil {
			return
		}
		r.mu.Lock()
		if fromClient {
			if r.first == nil {
				r.first = addr
			}
			r.peer = addr
		}
		r.count++
		drop := r.count%23 == 0 || (fromClient && r.dropFirst && addr.String() == r.first.String())
		delay := 12 * time.Millisecond
		if r.count%4 == 0 {
			delay = 22 * time.Millisecond
		}
		out, dest := r.front, r.peer
		if fromClient {
			out, dest = r.upstream, r.server
		}
		r.mu.Unlock()
		if drop {
			r.dropped.Add(1)
			continue
		}
		if dest == nil {
			continue
		}
		data := append([]byte(nil), buf[:n]...)
		time.AfterFunc(delay, func() { out.WriteToUDP(data, dest) })
	}
}

func TestRetryLostHandshake(t *testing.T) {
	f := setup(t)
	r := newRelay(f, true)
	f.run("a", client, "list")
	if r.dropped.Load() == 0 {
		t.Fatal("expected loss")
	}
}

func TestNetworkPath(t *testing.T) {
	f := setup(t)
	p, id := f.session(nil, "cat")
	r := newRelay(f, false)
	v := f.start("a", client, "attach", id)
	f.echo(v, "before-rebinding")
	p.see("before-rebinding\n")
	r.rebind(t)
	duration := 2 * time.Second
	if s := os.Getenv("CLISTREAM_SOAK_SECONDS"); s != "" {
		secs, err := strconv.ParseFloat(s, 64)
		check(t, err)
		duration = time.Duration(secs * float64(time.Second))
	}
	end := time.Now().Add(duration)
	for seq := 0; seq < 40 || time.Now().Before(end); seq++ {
		s := fmt.Sprintf("%09d:", seq) + strings.Repeat("x", 256)
		f.echo(v, s)
		p.see(s + "\n")
	}
	if r.dropped.Load() == 0 {
		t.Fatal("expected loss")
	}
}
