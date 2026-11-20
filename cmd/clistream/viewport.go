package main

import (
	"bytes"
	"context"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/wire"
)

type terminalRecord struct {
	data []byte
	done bool
	code int
	err  error
}
type viewport struct {
	term      *vt.Emulator
	remote    *client.Attachment
	mode      string
	output    chan terminalRecord
	cancel    context.CancelFunc
	inputDone chan struct{}
}

func newViewport(a *client.Attachment, mode string, w, h int) *viewport {
	ctx, cancel := context.WithCancel(context.Background())
	v := &viewport{term: vt.NewEmulator(w, h), remote: a, mode: mode, output: make(chan terminalRecord, 32), cancel: cancel, inputDone: make(chan struct{})}
	go func() {
		defer close(v.inputDone)
		// Keep draining terminal replies until shutdown even if a run has just exited.
		client.CopyInput(v.term, func(b []byte) error {
			if ctx.Err() == nil {
				a.Send(wire.Message{Op: "data", Data: b})
			}
			return nil
		})
	}()
	if mode == "pty" {
		v.sendSize(w, h)
	}
	go func() {
		defer close(v.output)
		code, err := a.Receive(func(_ string, data []byte) error {
			if mode == "pipe" {
				data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
			}
			select {
			case v.output <- terminalRecord{data: data}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		select {
		case v.output <- terminalRecord{done: true, code: code, err: err}:
		case <-ctx.Done():
		}
	}()
	return v
}
func (v *viewport) close() {
	v.cancel()
	v.remote.Close()
	v.term.InputPipe().(io.Closer).Close()
	<-v.inputDone
	v.term.Close()
}
func (v *viewport) sendSize(w, h int) {
	v.remote.Send(wire.Message{Op: "resize", Cols: uint16(w), Rows: uint16(h)})
}
func (v *viewport) resize(w, h int) {
	v.term.Resize(w, h)
	if v.mode == "pty" {
		v.sendSize(w, h)
	}
}
func (v *viewport) mouse(event uv.MouseEvent) {
	m := event.Mouse()
	m.Y -= 2
	if m.X < 0 || m.Y < 0 || m.X >= v.term.Width() || m.Y >= v.term.Height() {
		return
	}
	switch event.(type) {
	case uv.MouseClickEvent:
		v.term.SendMouse(uv.MouseClickEvent(m))
	case uv.MouseReleaseEvent:
		v.term.SendMouse(uv.MouseReleaseEvent(m))
	case uv.MouseMotionEvent:
		v.term.SendMouse(uv.MouseMotionEvent(m))
	case uv.MouseWheelEvent:
		v.term.SendMouse(uv.MouseWheelEvent(m))
	}
}
