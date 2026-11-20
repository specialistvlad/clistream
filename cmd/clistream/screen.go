package main

import (
	"fmt"
	"image"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/specialistvlad/clistream/internal/wire"
)

func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '�'
		}
		return r
	}, s)
}
func stateLabel(row wire.Supervisor) string {
	if row.Revision == 0 {
		return "Attach only"
	}
	if row.Failed {
		return "Launch failed"
	}
	if row.Info.ID != "" {
		return "Running"
	}
	if row.Exit != nil {
		return fmt.Sprintf("Stopped · exit %d", *row.Exit)
	}
	return "Stopped"
}
func prefixWidth(rows []wire.Supervisor) int {
	width := 6
	for i := 1; i < len(rows); i++ {
		a, b := rows[i-1].ID, rows[i].ID
		j := 0
		for j < min(len(a), len(b)) && a[j] == b[j] {
			j++
		}
		width = max(width, j+1)
	}
	return width
}
func (d *dashboardUI) draw() error {
	s := d.screen
	w, h := s.Width(), s.Height()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s.SetCell(x, y, &uv.EmptyCell)
		}
	}
	cell := func(x, y, end int, text string) {
		if x < end && y >= 0 && y < h {
			uv.NewStyledString(printable(text)).Draw(s, image.Rect(x, y, end, y+1))
		}
	}
	line := func(y int, text string) { cell(0, y, w, text) }
	title := "CLIStream Client"
	keys := "Q Quit"
	target := d.target.Info.Name + " on " + d.target.Info.Host
	s.HideCursor()
	switch d.page {
	case "connecting":
		title += " │ " + d.host
		line(2, string("|/-\\"[d.frame/3%4])+" Connecting to "+d.host)
	case "lost":
		title += " │ Disconnected from " + d.host
		line(2, d.notice)
		keys = "R Retry   Q Quit"
	case "attached":
		if w < 55 {
			title += " │ " + target
		} else {
			title += " │ Attached to " + target
		}
		v := d.attachment.term
		for y := 0; y < min(v.Height(), h-2); y++ {
			for x := 0; x < min(w, v.Width()); {
				c := v.CellAt(x, y)
				step := 1
				if c != nil {
					c = c.Clone()
					c.Link = uv.Link{}
					c.Content = printable(c.Content)
					s.SetCell(x, y+2, c)
					step = max(1, c.Width)
				}
				x += step
			}
		}
		if !v.CursorHidden() {
			p := v.CursorPosition()
			s.SetCursorPosition(p.X, p.Y+2)
			s.ShowCursor()
		}
		leave := "Home"
		if d.target.Revision == 0 {
			leave = "Disconnect"
		}
		keys = "Ctrl-G " + leave + "   Ctrl-[ Previous   Ctrl-] Next"
		if s.StringWidth(keys) > w {
			keys = "^G " + leave + "   ^[ Prev   ^] Next"
		}
		if d.notice != "" {
			title += " │ " + d.notice
		}
	case "confirm":
		verb := map[string]string{"apply": "Start with these arguments", "restart": "Restart", "stop": "Stop"}[d.action]
		if d.action == "apply" && d.target.Info.ID != "" {
			verb = "Restart with these arguments"
		}
		title += " │ " + target
		line(2, verb+" "+target+"?")
		choices := "[Cancel]    " + verb
		if d.confirm {
			choices = " Cancel    [" + verb + "]"
		}
		line(4, choices)
		keys = "Tab Select   Enter Confirm   Esc Cancel"
		if w < 40 {
			keys = "Tab Select  ↵ Confirm  Esc Cancel"
		}
	case "command":
		verb := "Start"
		if d.target.Info.ID != "" {
			verb = "Restart"
		}
		title += " │ " + verb + " " + target
		keys = "Enter " + verb + " & attach   Esc Cancel"
		if s.StringWidth(keys) > w {
			keys = "↵ " + verb + "   Esc Cancel"
		}
		line(3, "Command")
		before := printable(d.line.text[:d.line.pos])
		before = ansi.TruncateLeft(before, max(0, s.StringWidth(before)-max(0, w-3)), "")
		line(4, "› "+before+d.line.text[d.line.pos:])
		if h > 4 {
			s.SetCursorPosition(min(w-1, 2+s.StringWidth(before)), 4)
			s.ShowCursor()
		}
		line(6, d.notice)
	case "edit":
		title += " │ Edit arguments │ " + target
		line(2, "Executable   "+d.target.Info.Name)
		first := max(0, d.arg-max(0, h-5))
		for i := first; i < len(d.args) && 3+i-first < h; i++ {
			mark := "  "
			if i == d.arg {
				mark = "› "
			}
			prefix := fmt.Sprintf("%s%d  ", mark, i+1)
			text := d.args[i]
			if i == d.arg {
				runes := []rune(text)
				before := printable(string(runes[:d.cursor]))
				available := max(1, w-s.StringWidth(prefix)-1)
				before = ansi.TruncateLeft(before, max(0, s.StringWidth(before)-available), "")
				text = before + string(runes[d.cursor:])
				s.SetCursorPosition(min(w-1, s.StringWidth(prefix+before)), 3+i-first)
				s.ShowCursor()
			}
			line(3+i-first, prefix+text)
		}
		verb := "Start"
		if d.target.Info.ID != "" {
			verb = "Restart"
		}
		keys = "Ctrl-R " + verb + " with these arguments   ↑↓ Select   Enter Add   Ctrl-D Remove   Esc Cancel"
		if s.StringWidth(keys) > w {
			keys = "↑↓ Select  ↵ Add  ^D Remove  ^R " + verb + "  Esc Cancel"
		}
		if s.StringWidth(keys) > w {
			keys = "↑↓  ↵ Add  ^D Del  ^R Run  Esc"
		}
	case "table":
		if w < 55 {
			title += " │ " + d.host
		} else {
			title += " │ Supervisors │ Connected to " + d.host
		}
		if len(d.rows) == 0 {
			line(2, "Connected · Waiting for supervisors")
			break
		}
		idw := prefixWidth(d.rows)
		rowHeight := 1
		if w-idw < 35 {
			rowHeight = 2
		}
		wide := w >= 64
		nameW := max(3, (w-4)/3)
		hostW := nameW
		stateX := 2 + nameW + hostW
		if wide {
			nameW = (w - idw - 27) / 2
			hostW = w - idw - 27 - nameW
			stateX = 2 + nameW + hostW
		}
		row := func(y int, name, host, state, mode, id string) {
			if rowHeight == 2 {
				split := max(3, w/2)
				cell(2, y, split, name)
				cell(split, y, w, state)
				if y > 2 {
					cell(2, y+1, w-idw-1, host)
					cell(max(0, w-idw), y+1, w, id)
				}
				return
			}
			cell(2, y, 2+nameW, name)
			cell(2+nameW, y, stateX, host)
			stateEnd := w
			if wide {
				stateEnd = w - idw - 6
			} else if w >= 38 {
				stateEnd = w - idw
			}
			cell(stateX, y, stateEnd, state)
			if wide {
				cell(w-idw-6, y, w-idw, mode)
			}
			if w >= 38 {
				cell(w-idw, y, w, id)
			}
		}
		row(2, "Program", "Host", "State", "Mode", "ID")
		room := max(1, (h-4)/rowHeight)
		first := max(0, d.index-room+1)
		for i := first; i < len(d.rows) && i < first+room; i++ {
			v := d.rows[i]
			y := 3 + (i-first)*rowHeight
			if i == d.index {
				cell(0, y, 2, "›")
			}
			row(y, v.Info.Name, v.Info.Host, stateLabel(v), strings.ToUpper(v.Info.Mode), v.ID[:min(idw, len(v.ID))])
		}
		keys = "↑↓ Select"
		v := d.rows[d.index]
		if v.Info.ID != "" {
			keys += "   Enter Attach"
		}
		if v.Revision != 0 {
			if v.Info.ID != "" {
				keys += "   R Restart   S Stop"
			} else {
				keys += "   R Start"
			}
			if v.EditableCommand {
				keys += "   E Edit command"
			} else {
				keys += "   E Edit arguments"
			}
		}
		keys += "   Q Quit"
		if s.StringWidth(keys) > w {
			keys = strings.NewReplacer("   ", " ", "↑↓ Select", "↑↓", " Edit arguments", " Edit", " Edit command", " Edit").Replace(keys)
		}
		if s.StringWidth(keys) > w {
			keys = strings.NewReplacer("Enter Attach", "↵", "R Restart", "R", "R Start", "R", "S Stop", "S", "E Edit", "E", "Q Quit", "Q").Replace(keys)
		}
	}
	if d.page == "table" && d.notice != "" {
		line(h-1, d.notice)
	}
	line(0, title)
	if h > 1 {
		line(1, keys)
	}
	for y := 0; y < min(2, h); y++ {
		for x := 0; x < w; {
			c := s.CellAt(x, y).Clone()
			c.Style.Attrs |= uv.AttrReverse
			if y == 0 {
				c.Style.Attrs |= uv.AttrBold
			}
			s.SetCell(x, y, c)
			x += max(1, c.Width)
		}
	}
	s.Render()
	return s.Flush()
}
