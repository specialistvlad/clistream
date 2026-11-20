package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/cancelreader"
	"github.com/specialistvlad/clistream/internal/wire"
)

func (d *dashboardUI) size() (uint16, uint16) {
	return uint16(min(65535, max(1, d.width))), uint16(min(65535, max(1, d.height-2)))
}

type resultPage struct {
	ctx  context.Context
	text string
	code int
	in   io.Reader
	out  io.Writer
}

func (p *resultPage) SetStdin(r io.Reader)  { p.in = r }
func (p *resultPage) SetStdout(w io.Writer) { p.out = w }
func (p *resultPage) SetStderr(io.Writer)   {}
func (p *resultPage) Run() error {
	if _, err := fmt.Fprintf(p.out, "%s\nExit %d. Press Enter to return to sessions.\n", p.text, p.code); err != nil {
		return err
	}
	reader, err := cancelreader.NewReader(p.in)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(p.ctx, func() { reader.Cancel(); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		reader.Close()
	}()
	_, err = bufio.NewReader(reader).ReadString('\n')
	return err
}
func (d *dashboardUI) showResult(row wire.Supervisor, run string) {
	d.page = "table"
	s, _, err := wire.Open(d.conn, wire.Message{Op: "result", ID: row.ID, Revision: row.Revision, Run: run})
	if err != nil {
		d.notice = "Run output unavailable"
		return
	}
	defer s.CancelRead(0)
	s.Close()
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	var raw strings.Builder
	truncated := false
	for {
		m, err := wire.Read(s)
		if err != nil || m.Op == "error" {
			d.notice = "Run output unavailable"
			return
		}
		if m.Op == "truncated" {
			truncated = true
			continue
		}
		if m.Op == "exit" {
			text := raw.String()
			if m.Channel == "alternate" {
				d.notice = fmt.Sprintf("Child exited · exit %d", m.Code)
				return
			}
			text = strings.Map(func(r rune) rune {
				if r == '\n' || r == '\t' || unicode.IsPrint(r) {
					return r
				}
				return -1
			}, ansi.Strip(text))
			if truncated {
				text = "[Earlier output was discarded]\n" + text
			}
			d.pending = tea.Exec(&resultPage{ctx: d.ctx, text: text, code: m.Code}, nil)
			return
		}
		if m.Op != "data" || raw.Len()+len(m.Data) > 2<<20 {
			d.notice = "Invalid run output"
			return
		}
		raw.Write(m.Data)
	}
}
