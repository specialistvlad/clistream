package termio

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
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

// QuietInput keeps terminal-generated signals and discards queued typing on exit.
func QuietInput() (func(), error) {
	fd := int(os.Stdin.Fd())
	if group, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP); err == nil && group > 0 && group != unix.Getpgrp() {
		return func() {}, nil
	}
	prior, err := unix.IoctlGetTermios(fd, getMode)
	if err != nil {
		return nil, err
	}
	mode := *prior
	mode.Lflag &^= unix.ECHO | unix.ECHONL
	if err := unix.IoctlSetTermios(fd, setMode, &mode); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, restoreAndFlush, prior) }, nil
}
