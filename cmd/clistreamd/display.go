package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/specialistvlad/clistream/internal/identity"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func adminRequest(sock, action, arg string) (string, error) {
	c, e := net.DialTimeout("unix", sock, 3*time.Second)
	if e != nil {
		return "", e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if e = json.NewEncoder(c).Encode(request{action, arg}); e != nil {
		return "", e
	}
	var r reply
	if e = json.NewDecoder(c).Decode(&r); e != nil {
		return "", e
	}
	if r.Error != "" {
		return "", errors.New(r.Error)
	}
	return r.Result, nil
}
func printQR(link string) error {
	q, e := qrcode.New(link, qrcode.Medium)
	if e == nil {
		fmt.Print(q.ToSmallString(false))
	}
	return e
}

var safeCommandPath = regexp.MustCompile(`^[a-zA-Z0-9_./-]+$`)

// Preserve a relative invocation such as ./bin/clistreamd for the same-machine command.
func localClientCommand() string {
	directory, _ := filepath.Split(os.Args[0])
	command := directory + "clistream"
	if !safeCommandPath.MatchString(command) {
		return "'" + strings.ReplaceAll(command, "'", "'\"'\"'") + "'"
	}
	return command
}

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func display(ctx context.Context, qr bool, issue func() (string, error)) error {
	fd := int(os.Stdin.Fd())
	old, e := unix.IoctlGetTermios(fd, getTermios)
	if e != nil {
		return e
	}
	settings := *old
	settings.Lflag &^= unix.ICANON | unix.ECHO
	settings.Cc[unix.VMIN] = 1
	settings.Cc[unix.VTIME] = 0
	if e = unix.IoctlSetTermios(fd, setTermios, &settings); e != nil {
		return e
	}
	defer unix.IoctlSetTermios(fd, setTermios, old)
	var expires time.Time
	refresh := true
	for ctx.Err() == nil {
		if refresh {
			links, e := issue()
			if e != nil {
				return e
			}
			for _, link := range strings.Split(links, "\n") {
				i, e := identity.ParseInvitation(link)
				if e != nil {
					return e
				}
				host, _, _ := net.SplitHostPort(i.Address)
				label := "Network"
				command := "clistream"
				if net.ParseIP(host).IsLoopback() || host == "localhost" {
					label = "This computer only"
					command = localClientCommand()
				}
				fmt.Printf("%s (%s)\n  %s %s\n", label, i.Address, command, link)
				if qr {
					if e := printQR(link); e != nil {
						return e
					}
				}
				expires = i.Expires
			}
			fmt.Println("Run the printed command, or open the link in CLIStream and tap Pair device.")
			fmt.Println("To share a program, get a supervisor link in another terminal with clistreamd -qr=false invite supervisor.")
			fmt.Printf("Use one link to pair a device before %s. Press p for a fresh link. Ctrl-C exits.\n", expires.Local().Format("15:04:05 MST"))
			refresh = false
		}
		if !expires.IsZero() && !time.Now().Before(expires) {
			fmt.Println("Invitation expired. Press p for a new link.")
			expires = time.Time{}
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, e := unix.Poll(fds, 200)
		if e == unix.EINTR {
			continue
		}
		if e != nil {
			return e
		}
		if n > 0 {
			var b [1]byte
			n, e := os.Stdin.Read(b[:])
			if e != nil || n == 0 {
				return e
			}
			if b[0] == 'p' {
				refresh = true
			}
		}
	}
	return nil
}
