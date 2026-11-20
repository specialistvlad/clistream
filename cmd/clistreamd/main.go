package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/specialistvlad/clistream/internal/broker"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
	"golang.org/x/sys/unix"
)

type request struct{ Action, Arg string }
type reply struct{ Result, Error string }

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	dir := flag.String("state", identity.Default("server"), "private state directory")
	listen := flag.String("listen", "127.0.0.1:7443", "UDP listener")
	addr := flag.String("advertise", "", "host:port clients can reach")
	qr := flag.Bool("qr", true, "render invitation QR")
	var names []string
	flag.Func("interface", "interface name or loopback (repeatable)", func(name string) error { names = append(names, name); return nil })
	flag.Parse()
	action, arg := "serve", "client"
	if flag.NArg() > 0 {
		action = flag.Arg(0)
	}
	if flag.NArg() > 1 {
		arg = flag.Arg(1)
	}
	if e := os.MkdirAll(*dir, 0700); e != nil {
		return e
	}
	sock := filepath.Join(*dir, "admin.sock")
	lock, e := os.OpenFile(filepath.Join(*dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		if !errors.Is(e, unix.EWOULDBLOCK) {
			return e
		}
		if action == "serve" && interactive() {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer cancel()
			return display(ctx, *qr, func() (string, error) { return adminRequest(sock, "display", "client") })
		}
		if action == "serve" {
			fmt.Println("clistreamd is already running. Open clistreamd in a terminal to pair, or run clistreamd invite client.")
			return nil
		}
		result, e := adminRequest(sock, action, arg)
		if e != nil {
			return e
		}
		if action == "invite" && *qr {
			if e := printQR(result); e != nil {
				return e
			}
		}
		fmt.Println(result)
		return nil
	}
	if action != "serve" {
		return errors.New("start clistreamd first")
	}
	configs, e := endpoints(names, *listen, *addr)
	if e != nil {
		return e
	}
	var addresses []string
	var sockets []net.PacketConn
	for _, cfg := range configs {
		pc, e := net.ListenPacket("udp", cfg.bind)
		if e != nil {
			return e
		}
		defer pc.Close()
		sockets = append(sockets, pc)
		adv := cfg.advertised
		h, port, e := net.SplitHostPort(adv)
		if e != nil || h == "" || net.ParseIP(h).IsUnspecified() {
			return errors.New("invalid advertised endpoint")
		}
		if port == "0" {
			_, bound, _ := net.SplitHostPort(pc.LocalAddr().String())
			adv = net.JoinHostPort(h, bound)
		}
		addresses = append(addresses, adv)
	}
	s, t, e := broker.New(*dir, addresses, identity.DeviceKeys{})
	if e != nil {
		return e
	}
	var listeners []*quic.Listener
	for _, pc := range sockets {
		l, e := quic.Listen(pc, t, wire.Config())
		if e != nil {
			return e
		}
		defer l.Close()
		listeners = append(listeners, l)
	}
	_ = os.Remove(sock)
	admin, e := net.Listen("unix", sock)
	if e != nil {
		return e
	}
	defer admin.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	go func() {
		for {
			c, e := admin.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				var r request
				if json.NewDecoder(bufio.NewReader(c)).Decode(&r) != nil {
					return
				}
				result, e := s.Admin(r.Action, r.Arg)
				out := reply{Result: result}
				if e != nil {
					out.Error = e.Error()
				}
				json.NewEncoder(c).Encode(out)
			}()
		}
	}()
	for _, l := range listeners {
		fmt.Fprintln(os.Stderr, "serving QUIC on", l.Addr())
	}
	if interactive() {
		done := make(chan error, 1)
		go func() { done <- s.Serve(ctx, listeners...); cancel() }()
		e := display(ctx, *qr, func() (string, error) { return s.Admin("display", "client") })
		cancel()
		<-done
		return e
	}
	e = s.Serve(ctx, listeners...)
	if ctx.Err() != nil {
		return nil
	}
	return e
}
