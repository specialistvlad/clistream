package main

import (
	"fmt"
	"github.com/specialistvlad/clistream/internal/terminalview"
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/specialistvlad/clistream/internal/wire"
)

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
func (d *dashboardUI) View() tea.View {
	w, h := d.width, d.height
	s := uv.NewScreenBuffer(w, h)
	view := tea.View{AltScreen: true}
	cell := func(x, y, end int, text string) {
		if x < end && y >= 0 && y < h {
			uv.NewStyledString(terminalview.Printable(text)).Draw(&s, image.Rect(x, y, end, y+1))
		}
	}
	line := func(y int, text string) { cell(0, y, w, text) }
	title := "CLIStream Client"
	keys := "Q Quit"
	target := d.target.Info.Name + " on " + d.target.Info.Host

	switch d.page {
	case "connecting":
		title += " │ " + d.host
		line(2, string("|/-\\"[d.frame/3%4])+" Connecting to "+d.host)
	case "lost":
		title += " │ Disconnected from " + d.host
		line(2, d.notice)
		keys = "R Retry   Q Quit"
	case "result":
		title = fmt.Sprintf("%s │ Exit %d │ %s", title, d.exitCode, target)
		keys = "Enter Home   ↑↓ Scroll   PgUp/PgDn Page   Home/End"
		if h > 2 {
			uv.NewStyledString(d.output.View()).Draw(&s, image.Rect(0, 2, w, h))
		}
	case "attached":
		if w < 55 {
			title += " │ " + target
		} else {
			title += " │ Attached to " + target
		}
		view.MouseMode = tea.MouseModeAllMotion
		v := d.attachment.term
		terminalview.Draw(&s, v, &view)
		leave := "Home"
		if d.target.Revision == 0 {
			leave = "Disconnect"
		}
		keys = "Ctrl-G " + leave + "   Ctrl-[ Previous   Ctrl-] Next"
		if ansi.StringWidth(keys) > w {
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
		if ansi.StringWidth(keys) > w {
			keys = "↵ " + verb + "   Esc Cancel"
		}
		// Use a bordered field when there is room, and keep the input visible on short terminals.
		fieldY := min(4, h-1)
		framed := h >= 7 && w >= 12
		inset := 0
		if framed {
			inset = 1
		}
		if fieldY >= 4 {
			line(2, "Command")
		}
		if width := max(1, w-2*inset-3); d.line.Width() != width {
			d.line.SetWidth(width)
			d.line.SetCursor(d.line.Position())
		}
		if fieldY >= 2 && w > 2 {
			field := d.line.View()
			top := fieldY
			if framed {
				field = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Render(field)
				top--
			}
			uv.NewStyledString(field).Draw(&s, image.Rect(0, top, w, fieldY+inset+1))
			view.Cursor = d.line.Cursor()
			view.Cursor.X += inset
			view.Cursor.Y = fieldY
		}
		line(fieldY+inset+1, d.notice)
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
				before := terminalview.Printable(string(runes[:d.cursor]))
				available := max(1, w-ansi.StringWidth(prefix)-1)
				before = ansi.TruncateLeft(before, max(0, ansi.StringWidth(before)-available), "")
				text = before + string(runes[d.cursor:])
				view.Cursor = tea.NewCursor(min(w-1, ansi.StringWidth(prefix+before)), 3+i-first)

			}
			line(3+i-first, prefix+text)
		}
		verb := "Start"
		if d.target.Info.ID != "" {
			verb = "Restart"
		}
		keys = "Ctrl-R " + verb + " with these arguments   ↑↓ Select   Enter Add   Ctrl-D Remove   Esc Cancel"
		if ansi.StringWidth(keys) > w {
			keys = "↑↓ Select  ↵ Add  ^D Remove  ^R " + verb + "  Esc Cancel"
		}
		if ansi.StringWidth(keys) > w {
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
		} else if v.RunResult && v.Exit != nil {
			keys += "   Enter Output"
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
		if ansi.StringWidth(keys) > w {
			keys = strings.NewReplacer("   ", " ", "↑↓ Select", "↑↓", " Edit arguments", " Edit", " Edit command", " Edit").Replace(keys)
		}
		if ansi.StringWidth(keys) > w {
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
	view.Content = s.Render()
	return view
}
