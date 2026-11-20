package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/wire"
)

func chooseSession(c *quic.Conn, items []wire.Session) (wire.Session, error) {
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "Connected. Waiting for a supervisor. Press Ctrl-C to exit.")
	}
	for len(items) == 0 {
		select {
		case <-c.Context().Done():
			return wire.Session{}, fmt.Errorf("server disconnected")
		case <-time.After(time.Second):
		}
		var e error
		items, e = client.List(c)
		if e != nil {
			return wire.Session{}, e
		}
	}
	if len(items) == 1 {
		return items[0], nil
	}
	for i, s := range items {
		fmt.Fprintf(os.Stderr, "%d. %q on %q (%s)\n", i+1, s.Name, s.Host, s.Mode)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "Choose a session by number: ")
		line, e := reader.ReadString('\n')
		if e != nil {
			return wire.Session{}, e
		}
		n, e := strconv.Atoi(strings.TrimSpace(line))
		if e == nil && n > 0 && n <= len(items) {
			return items[n-1], nil
		}
	}
}
