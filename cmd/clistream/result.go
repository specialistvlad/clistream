package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	pager "charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/specialistvlad/clistream/internal/wire"
)

func (d *dashboardUI) size() (uint16, uint16) {
	return uint16(min(65535, max(1, d.width))), uint16(min(65535, max(1, d.height-2)))
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
			d.target = row
			d.page = "result"
			d.exitCode = m.Code
			d.output = pager.New()
			d.transcript = strings.ReplaceAll(text, "\t", "        ")
			d.resizeOutput()
			return
		}
		if m.Op != "data" || raw.Len()+len(m.Data) > 2<<20 {
			d.notice = "Invalid run output"
			return
		}
		raw.Write(m.Data)
	}
}

func (d *dashboardUI) resizeOutput() {
	width := max(1, d.width)
	if d.output.Width() != width {
		d.output.SetWidth(width)
		d.output.SetContent(ansi.Hardwrap(d.transcript, width, true))
	}
	d.output.SetHeight(max(1, d.height-2))
	d.output.SetYOffset(d.output.YOffset())
}
