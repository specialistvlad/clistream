package session

import (
	"context"
	"errors"
	"sync"

	"github.com/specialistvlad/clistream/internal/wire"
)

const historyBytes = 1024 * 1024

type history struct {
	mu       sync.Mutex
	next     chan struct{}
	chunks   [][]byte
	start    int64
	size     int
	finished bool
	code     int
}

func newHistory() *history { return &history{next: make(chan struct{})} }
func (h *history) wake()   { close(h.next); h.next = make(chan struct{}) }
func (h *history) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for left := p; len(left) > 0; {
		n := min(len(left), 32768)
		h.chunks = append(h.chunks, append([]byte(nil), left[:n]...))
		h.size += n
		left = left[n:]
	}
	for h.size > historyBytes || len(h.chunks) > 4096 {
		h.size -= len(h.chunks[0])
		h.chunks[0] = nil
		h.chunks = h.chunks[1:]
		h.start++
	}
	h.wake()
	return len(p), nil
}
func (h *history) finish(code int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finished = true
	h.code = code
	h.wake()
}
func (h *history) stream(ctx context.Context, send func(wire.Message) error) error {
	h.mu.Lock()
	pos := h.start
	h.mu.Unlock()
	for {
		h.mu.Lock()
		i := pos - h.start
		if i < 0 {
			h.mu.Unlock()
			return errors.New("output exceeded replay buffer; reattach")
		}
		if i < int64(len(h.chunks)) {
			b := h.chunks[i]
			h.mu.Unlock()
			if e := send(wire.Message{Op: "data", Data: b}); e != nil {
				return e
			}
			pos++
			continue
		}
		ch, end, code := h.next, h.finished, h.code
		h.mu.Unlock()
		if end {
			return send(wire.Message{Op: "exit", Code: code})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}
