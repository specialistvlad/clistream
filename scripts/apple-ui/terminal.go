package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"golang.org/x/term"
)

var pointerPress = regexp.MustCompile(`\x1b\[<0;[0-9]+;[0-9]+M`)

// terminalChild reports the grid observed by the child and the exact bytes
// written by the terminal. It runs under the real supervisor PTY.
func terminalChild() error {
	fd := int(os.Stdin.Fd())
	saved, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, saved)
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	sizes, err := os.OpenFile("grid", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer sizes.Close()
	grid := func() {
		w, h, e := term.GetSize(fd)
		if e == nil {
			fmt.Fprintf(sizes, "%d %d\n", w, h)
			fmt.Printf("\r\ngrid=%dx%d\r\n", w, h)
		}
	}
	grid()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-resize:
				grid()
			case <-done:
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
				if err := os.Remove("reset-terminal"); err == nil {
					fmt.Print("\x1b[2J\x1b[HCOPYME terminal selection test\r\nscroll-ready\r\nTerminal ready\r\n\x1b[?1000h\x1b[?1006h")
				}
				if err := os.Remove("history"); err == nil {
					fmt.Print("\x1b[?1000l\x1b[?1006l")
					for i := 0; i < 350; i++ {
						fmt.Printf("history-%03d\r\n", i)
					}
					fmt.Print("history-end")
				}
			}
		}
	}()
	file, err := os.OpenFile("keys", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	var clicks int
	var tail []byte
	fmt.Print("COPYME terminal selection test\r\nscroll-ready\r\nTerminal ready\r\n\x1b[?1000h\x1b[?1006h")
	var buf [128]byte
	for {
		n, e := os.Stdin.Read(buf[:])
		if e != nil {
			return e
		}
		file.Write(buf[:n])
		data := append(tail, buf[:n]...)
		for _, match := range pointerPress.FindAllIndex(data, -1) {
			if match[1] > len(tail) {
				clicks++
				fmt.Printf("\r\npointer-%d\r\n", clicks)
			}
		}
		tail = bytes.Clone(data[max(0, len(data)-32):])
		for _, b := range buf[:n] {
			fmt.Printf("[%02x]", b)
		}
	}
}
