package termio

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

func Setup(resize func(uint16, uint16)) func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	old, e := term.MakeRaw(fd)
	if e != nil {
		return func() {}
	}
	update := func() {
		w, h, e := term.GetSize(fd)
		if e == nil && w > 0 && h > 0 {
			resize(uint16(w), uint16(h))
		}
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	done := make(chan struct{})
	update()
	go func() {
		for {
			select {
			case <-sig:
				update()
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(sig); close(done); term.Restore(fd, old) }
}
