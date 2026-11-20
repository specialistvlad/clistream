package main

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/rivo/uniseg"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Quote control characters and arbitrary Unix argument bytes into one printable line.
func formatCommand(argv [][]byte) string {
	parts := make([]string, len(argv))
	for i, b := range argv {
		s := string(b)
		if s == "" || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool {
			return !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune("'\"\\", r)
		}) >= 0 {
			s = strconv.Quote(s)
		}
		parts[i] = s
	}
	return strings.Join(parts, " ")
}

func parseCommand(line string) ([][]byte, error) {
	var argv [][]byte
	var word strings.Builder
	var quote rune
	started := false
	for line != "" {
		r, n := utf8.DecodeRuneInString(line)
		line = line[n:]
		switch {
		case quote == 0 && unicode.IsSpace(r):
			if started {
				argv = append(argv, []byte(word.String()))
				word.Reset()
				started = false
			}
			continue
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case r == '\\' && quote != '\'':
			if line == "" {
				return nil, errors.New("Complete the trailing escape.")
			}
			if quote == '"' {
				value, multi, rest, err := strconv.UnquoteChar("\\"+line, '"')
				if err != nil {
					return nil, errors.New("Invalid escape in double quotes.")
				}
				if multi {
					word.WriteRune(value)
				} else {
					word.WriteByte(byte(value))
				}
				line = rest
			} else {
				v, size := utf8.DecodeRuneInString(line)
				word.WriteRune(v)
				line = line[size:]
			}
		default:
			word.WriteRune(r)
		}
		started = true
	}
	if quote != 0 {
		return nil, errors.New("Close the unfinished quote.")
	}
	if started {
		argv = append(argv, []byte(word.String()))
	}
	if len(argv) == 0 || len(argv[0]) == 0 {
		return nil, errors.New("Enter an executable.")
	}
	return argv, nil
}

type commandLine struct {
	text string
	pos  int
}

func (l *commandLine) insert(text string) {
	l.text = l.text[:l.pos] + text + l.text[l.pos:]
	l.pos += len(text)
}

func (l commandLine) adjacent(left bool) int {
	if left {
		g := uniseg.NewGraphemes(l.text[:l.pos])
		last := 0
		for g.Next() {
			last, _ = g.Positions()
		}
		return last
	}
	g := uniseg.NewGraphemes(l.text[l.pos:])
	if g.Next() {
		_, end := g.Positions()
		return l.pos + end
	}
	return l.pos
}

func (l commandLine) word(left bool) int {
	// Word shortcuts use whitespace boundaries, like a shell's Ctrl-W.
	if left {
		for l.pos > 0 {
			r, n := utf8.DecodeLastRuneInString(l.text[:l.pos])
			if !unicode.IsSpace(r) {
				break
			}
			l.pos -= n
		}
		for l.pos > 0 {
			r, n := utf8.DecodeLastRuneInString(l.text[:l.pos])
			if unicode.IsSpace(r) {
				break
			}
			l.pos -= n
		}
	} else {
		for l.pos < len(l.text) {
			r, n := utf8.DecodeRuneInString(l.text[l.pos:])
			if !unicode.IsSpace(r) {
				break
			}
			l.pos += n
		}
		for l.pos < len(l.text) {
			r, n := utf8.DecodeRuneInString(l.text[l.pos:])
			if unicode.IsSpace(r) {
				break
			}
			l.pos += n
		}
	}
	return l.pos
}

func (l *commandLine) remove(to int) {
	a, b := min(to, l.pos), max(to, l.pos)
	l.text = l.text[:a] + l.text[b:]
	l.pos = a
}

func (l *commandLine) key(ev uv.KeyPressEvent) {
	switch ev.String() {
	case "left", "ctrl+b":
		l.pos = l.adjacent(true)
	case "right", "ctrl+f":
		l.pos = l.adjacent(false)
	case "home", "ctrl+a":
		l.pos = 0
	case "end", "ctrl+e":
		l.pos = len(l.text)
	case "ctrl+left", "alt+b":
		l.pos = l.word(true)
	case "ctrl+right", "alt+f":
		l.pos = l.word(false)
	case "ctrl+w", "alt+backspace":
		l.remove(l.word(true))
	case "alt+d":
		l.remove(l.word(false))
	case "ctrl+u":
		l.remove(0)
	case "ctrl+k":
		l.remove(len(l.text))
	case "backspace":
		l.remove(l.adjacent(true))
	case "delete", "ctrl+d":
		l.remove(l.adjacent(false))
	default:
		if ev.Text != "" {
			l.insert(ev.Text)
		}
	}
}

func (d *dashboardUI) openCommand() {
	res, err := d.request("command")
	if err != nil {
		d.notice = "Command unavailable"
		return
	}
	text := formatCommand(res.Command)
	d.line = commandLine{text, len(text)}
	d.page = "command"
}

func (d *dashboardUI) submitCommand() {
	argv, err := parseCommand(d.line.text)
	if err != nil {
		d.notice = err.Error()
		return
	}
	res, err := wire.Request(d.conn, wire.Message{Op: "run-command", ID: d.target.ID, Run: d.target.Info.ID, Revision: d.target.Revision, Command: argv})
	if res.Error == "invalid command" {
		d.notice = "Check the executable and driver on the host."
		return
	}
	d.page = "table"
	if e := d.refresh(); e != nil {
		d.disconnect(e)
		return
	}
	for i, row := range d.rows {
		if row.ID != d.target.ID {
			continue
		}
		d.index = i
		if res.Error == "child launch failed" && row.Revision == res.Revision {
			d.page = "command"
			d.target = row
			d.notice = "Launch failed. Edit the command to try again."
			return
		}
		if err == nil && res.Run != "" && row.Info.ID == res.Run {
			d.line = commandLine{}
			if d.attach(row) != nil {
				d.notice = "Run ended before attachment."
			}
			return
		}
	}
	d.line = commandLine{}
	d.notice = "Request ended. Check the current run."
}
