// Package main exposes the shared Go client to Swift through a small C ABI.
package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/forward"
	"github.com/specialistvlad/clistream/internal/identity"
	"github.com/specialistvlad/clistream/internal/wire"
)

type request struct {
	Op, Dir, Link, ID, Name, Handle, Path, Local string
	Data                                         []byte
	Cols, Rows                                   uint16
}
type response struct {
	Entries                         []wire.FileEntry `json:",omitempty"`
	Error, Handle, Channel, Address string           `json:",omitempty"`
	Sessions                        []wire.Session   `json:",omitempty"`
	Data                            []byte           `json:",omitempty"`
	Exit                            *int             `json:",omitempty"`
}
type handle struct {
	conn     *quic.Conn
	attach   *client.Attachment
	events   chan response
	done     chan struct{}
	listener *forward.Listener
}

var handles sync.Map

func (h *handle) emit(r response) bool {
	select {
	case h.events <- r:
		return true
	case <-h.done:
		return false
	}
}
func (h *handle) run() {
	code, e := h.attach.Receive(func(ch string, data []byte) error {
		if !h.emit(response{Channel: ch, Data: data}) {
			return context.Canceled
		}
		return nil
	})
	r := response{Exit: &code}
	if e != nil {
		r.Error = e.Error()
	}
	h.emit(r)
}
func call(q request) (response, error) {
	var r response
	switch q.Op {
	case "pair":
		return r, identity.Pair(context.Background(), q.Dir, q.Link)
	case "ls", "get", "put":
		var c *quic.Conn
		var e error
		if q.Handle != "" {
			value, ok := handles.Load(q.Handle)
			if !ok {
				return r, errors.New("attachment closed")
			}
			h := value.(*handle)
			if h.attach == nil {
				return r, errors.New("terminal attachment required")
			}
			c = h.conn
		} else {
			c, e = client.Connect(q.Dir)
			if e != nil {
				return r, e
			}
			defer c.CloseWithError(0, "")
		}
		switch q.Op {
		case "ls":
			e = client.Files(c, q.ID, q.Path, func(i wire.FileEntry) error {
				if len(r.Entries) >= 50000 {
					return errors.New("directory exceeds display limit")
				}
				r.Entries = append(r.Entries, i)
				return nil
			})
		case "get":
			e = client.Get(c, q.ID, q.Path, q.Local)
		case "put":
			e = client.Put(c, q.ID, q.Path, q.Local)
		}
		return r, e
	case "list", "attach", "forward":
		c, e := client.Connect(q.Dir)
		if e != nil {
			return r, e
		}
		if q.Op == "list" {
			defer c.CloseWithError(0, "")
			r.Sessions, e = client.List(c)
			return r, e
		}
		h := &handle{conn: c, events: make(chan response, 32), done: make(chan struct{})}
		if q.Op == "attach" {
			h.attach, e = client.Attach(c, q.ID)
			if e == nil {
				go h.run()
			}
		} else {
			var items []wire.Session
			items, e = client.List(c)
			if e == nil {
				e = errors.New("unknown export")
				for _, s := range items {
					if proto, ok := s.Ports[q.Name]; s.ID == q.ID && ok {
						h.listener, e = forward.Start(c, q.ID, q.Name, proto, "127.0.0.1:0")
						break
					}
				}
			}
			if e == nil {
				r.Address = h.listener.Address
			}
		}
		if e != nil {
			c.CloseWithError(0, "")
			return r, e
		}
		b := make([]byte, 16)
		rand.Read(b)
		r.Handle = hex.EncodeToString(b)
		handles.Store(r.Handle, h)
		return r, nil
	}
	v, ok := handles.Load(q.Handle)
	if !ok {
		return r, errors.New("attachment closed")
	}
	h := v.(*handle)
	switch q.Op {
	case "close":
		if _, ok := handles.LoadAndDelete(q.Handle); ok {
			close(h.done)
			if h.listener != nil {
				h.listener.Close()
			}
			h.conn.CloseWithError(0, "detached")
		}
		return r, nil
	}
	if h.attach == nil {
		return r, errors.New("terminal attachment required")
	}
	switch q.Op {
	case "next":
		select {
		case r = <-h.events:
			return r, nil
		case <-h.done:
			return r, context.Canceled
		}
	case "input":
		return r, h.attach.Send(wire.Message{Op: "data", Data: q.Data})
	case "resize":
		return r, h.attach.Send(wire.Message{Op: "resize", Cols: q.Cols, Rows: q.Rows})
	case "eof":
		return r, h.attach.Send(wire.Message{Op: "eof"})
	}
	return r, fmt.Errorf("unknown client operation %q", q.Op)
}

//export CSCall
func CSCall(raw *C.char) *C.char {
	var q request
	var r response
	e := json.Unmarshal([]byte(C.GoString(raw)), &q)
	if e == nil {
		r, e = call(q)
	}
	if e != nil {
		r.Error = e.Error()
	}
	b, _ := json.Marshal(r)
	return C.CString(string(b))
}
func main() {}
