package main

import (
	"fmt"
	"strings"

	pager "charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/wire"
)

func (d *dashboardUI) size() (uint16, uint16) {
	return uint16(min(65535, max(1, d.width))), uint16(min(65535, max(1, d.height-2)))
}

func (d *dashboardUI) showResult(row wire.Supervisor, run string) {
	d.page = "table"
	result, err := client.Result(d.conn, row.ID, run, row.Revision)
	if err != nil {
		d.notice = "Run output unavailable"
		return
	}
	if result.Alternate {
		d.notice = fmt.Sprintf("Child exited · exit %d", result.Exit)
		return
	}
	d.target = row
	d.page = "result"
	d.exitCode = result.Exit
	d.output = pager.New()
	d.transcript = strings.ReplaceAll(result.Text, "\t", "        ")
	d.resizeOutput()
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
