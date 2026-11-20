// Package terminalview draws child terminal cells below a two-row status header.
package terminalview

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return '�'
		}
		return r
	}, s)
}

func Draw(s *uv.ScreenBuffer, v *vt.Emulator, view *tea.View) {
	for y := 0; y < min(v.Height(), s.Height()-2); y++ {
		for x := 0; x < min(s.Width(), v.Width()); {
			c := v.CellAt(x, y)
			step := 1
			if c != nil {
				c = c.Clone()
				c.Link = uv.Link{}
				c.Content = Printable(c.Content)
				s.SetCell(x, y+2, c)
				step = max(1, c.Width)
			}
			x += step
		}
	}
	if !v.CursorHidden() {
		p := v.CursorPosition()
		view.Cursor = tea.NewCursor(p.X, p.Y+2)
	}
}

func Mouse(term *vt.Emulator, event uv.MouseEvent) {
	m := event.Mouse()
	m.Y -= 2
	if m.X < 0 || m.Y < 0 || m.X >= term.Width() || m.Y >= term.Height() {
		return
	}
	switch event.(type) {
	case uv.MouseClickEvent:
		term.SendMouse(uv.MouseClickEvent(m))
	case uv.MouseReleaseEvent:
		term.SendMouse(uv.MouseReleaseEvent(m))
	case uv.MouseMotionEvent:
		term.SendMouse(uv.MouseMotionEvent(m))
	case uv.MouseWheelEvent:
		term.SendMouse(uv.MouseWheelEvent(m))
	}
}
