package session

import (
	"bytes"
	"context"
	"testing"

	"github.com/specialistvlad/clistream/internal/wire"
)

func TestSlowSubscriberCanReattachWithinBoundedHistory(t *testing.T) {
	h := newHistory()
	h.Write([]byte("initial"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.stream(ctx, func(wire.Message) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	data := bytes.Repeat([]byte{0x71}, historyBytes+32768)
	h.Write(data)
	close(release)
	if err := <-done; err == nil {
		t.Fatal("overrun must be reported to the slow subscriber")
	}
	h.finish(7)
	var replay bytes.Buffer
	var exitCode int
	err := h.stream(ctx, func(m wire.Message) error {
		switch m.Op {
		case "data":
			replay.Write(m.Data)
		case "exit":
			exitCode = m.Code
		}
		return nil
	})
	if err != nil || exitCode != 7 || !bytes.Equal(replay.Bytes(), data[len(data)-historyBytes:]) {
		t.Fatalf("replay length %d, exit %d, err %v", replay.Len(), exitCode, err)
	}
}
