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
	"github.com/skip2/go-qrcode"
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
		if action == "serve" {
			action = "invite"
		}
		c, e := net.DialTimeout("unix", sock, 3*time.Second)
		if e != nil {
			return e
		}
		defer c.Close()
		if e = json.NewEncoder(c).Encode(request{action, arg}); e != nil {
			return e
		}
		var r reply
		if e = json.NewDecoder(c).Decode(&r); e != nil {
			return e
		}
		if r.Error != "" {
			return errors.New(r.Error)
		}
		if action == "invite" && *qr {
			q, e := qrcode.New(r.Result, qrcode.Medium)
			if e != nil {
				return e
			}
			fmt.Print(q.ToSmallString(false))
		}
		fmt.Println(r.Result)
		return nil
	}
	if action != "serve" {
		return errors.New("start clistreamd first")
	}
	if *addr == "" {
		*addr = *listen
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil || host == "" || net.ParseIP(host).IsUnspecified() {
		return errors.New("set -advertise to a reachable host:port")
	}
	s, t, e := broker.New(*dir, *addr, identity.DeviceKeys{})
	if e != nil {
		return e
	}
	l, e := quic.ListenAddr(*listen, t, wire.Config())
	if e != nil {
		return e
	}
	defer l.Close()
	_ = os.Remove(sock)
	admin, e := net.Listen("unix", sock)
	if e != nil {
		return e
	}
	defer admin.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	fmt.Fprintln(os.Stderr, "serving QUIC on", l.Addr())
	e = s.Serve(ctx, l)
	if ctx.Err() != nil {
		return nil
	}
	return e
}
