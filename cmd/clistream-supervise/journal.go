package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/specialistvlad/clistream/internal/activity"
	"github.com/specialistvlad/clistream/internal/identity"
)

type journalLocation string
type journalFailure string

func (c *console) openJournal(dir, id string) {
	c.instance = id
	folder := filepath.Join(dir, "logs")
	path := filepath.Join(folder, "supervisor-"+id+".log")
	diagnostic := func(err error) {
		msg := "Supervisor activity log unavailable: " + err.Error()
		if c.program != nil {
			c.program.Send(journalFailure(msg))
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
	}
	err := identity.EnsurePrivateDir(folder)
	if err != nil {
		diagnostic(err)
	} else if c.program != nil {
		c.program.Send(journalLocation(path))
	} else if c.logOnly {
		fmt.Fprintf(os.Stderr, "Activity log %q\n", path)
	}
	c.journal = activity.OpenJournal(path, err != nil, func(e activity.Event, _ []byte) {
		if c.logOnly {
			fmt.Fprintf(os.Stderr, "%s  %s\n", e.Time.Local().Format("15:04:05"), eventText(e))
		}
	}, diagnostic)
}

func (c *console) record(e activity.Event) {
	e.Supervisor = c.instance
	if len(e.Session) != 24 {
		e.Session = ""
	} else if _, err := hex.DecodeString(e.Session); err != nil {
		e.Session = ""
	}
	c.journal.Record(e)
}

func (c *console) exit(id string, code int) {
	c.record(activity.Event{Name: "child_exited", Session: id, ExitCode: &code})
}

var labels = map[string]string{
	"child_started":     "Child started",
	"launch_failed":     "Child launch failed",
	"connecting":        "Connecting",
	"connected":         "Connected",
	"connection_failed": "Connection failed",
	"connection_lost":   "Connection lost",

	"stdin":    "Terminal input",
	"stdout":   "Terminal output",
	"stderr":   "Terminal error output",
	"pty":      "Terminal output",
	"fs/get":   "File download",
	"fs/put":   "File upload",
	"fs/list":  "File listing",
	"port/tcp": "TCP forward",
	"port/udp": "UDP forward",
	"unknown":  "Unknown channel",
}

func eventText(e activity.Event) string {
	switch e.Name {
	case "child_exited":
		return fmt.Sprintf("Child exited (status %d)", *e.ExitCode)
	case "retry":
		return fmt.Sprintf("Reconnecting in %ds", e.RetrySeconds)
	case "channel":
		return labels[e.Operation] + " " + e.Outcome
	case "remote_request":
		action := e.Operation
		switch action {
		case "command", "arguments":
			action = "command lookup"
		case "run-command", "apply":
			action = "start"
			if e.Session != "" {
				action = "restart"
			}
		}
		return "Remote " + action + " " + e.Outcome
	case "registered":
		return strings.TrimSpace("Registered " + e.Session)
	default:
		return labels[e.Name]
	}
}
