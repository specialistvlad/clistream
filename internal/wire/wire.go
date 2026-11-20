// Package wire defines the complete v1 application record format.
package wire

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang/snappy"
	"github.com/quic-go/quic-go"
)

const ALPN = "clistream/1+snappy"
const MaxPlain = 128 * 1024

type Session struct {
	ID, Name, Mode, Driver, Version, Host, OS string
	Ports                                     map[string]string
}
type FileEntry struct {
	Name           string
	Directory      bool
	Size, Modified int64
}
type Message struct {
	Path                      string     `json:",omitempty"`
	Entry                     *FileEntry `json:",omitempty"`
	Op                        string
	Token, ID, Channel, Error string    `json:",omitempty"`
	Info                      *Session  `json:",omitempty"`
	Sessions                  []Session `json:",omitempty"`
	Data                      []byte    `json:",omitempty"`
	Cols, Rows                uint16    `json:",omitempty"`
	Code                      int       `json:",omitempty"`
}

func Write(w io.Writer, m Message) error {
	raw, e := json.Marshal(m)
	if e != nil {
		return e
	}
	if len(raw) > MaxPlain {
		return errors.New("oversized record")
	}
	b := snappy.Encode(nil, raw)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(b)))
	_, e = w.Write(append(length[:], b...))
	return e
}
func Read(r io.Reader) (m Message, err error) {
	var length [4]byte
	if _, err = io.ReadFull(r, length[:]); err != nil {
		return
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || int(n) > snappy.MaxEncodedLen(MaxPlain) {
		return m, errors.New("invalid compressed length")
	}
	b := make([]byte, n)
	if _, err = io.ReadFull(r, b); err != nil {
		return
	}
	size, e := snappy.DecodedLen(b)
	if e != nil || size > MaxPlain {
		return m, errors.New("invalid decoded length")
	}
	raw, e := snappy.Decode(nil, b)
	if e != nil {
		return m, e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	if d.Decode(new(any)) != io.EOF || m.Op == "" {
		return m, errors.New("invalid message")
	}
	return m, nil
}
func Fail(w io.Writer, e error) { _ = Write(w, Message{Op: "error", Error: e.Error()}) }
func Open(c *quic.Conn, m Message) (*quic.Stream, Message, error) {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	s, e := c.OpenStreamSync(ctx)
	if e != nil {
		return nil, Message{}, e
	}
	s.SetDeadline(time.Now().Add(10 * time.Second))
	e = Write(s, m)
	var res Message
	if e == nil {
		res, e = Read(s)
	}
	if e == nil && res.Op != "ok" {
		e = fmt.Errorf("request %s rejected: %s", m.Op, res.Error)
	}
	if e != nil {
		Abort(s)
		return nil, res, e
	}
	s.SetDeadline(time.Time{})
	return s, res, nil
}
func Abort(s *quic.Stream) { s.CancelRead(1); s.CancelWrite(1) }
func Config() *quic.Config {
	return &quic.Config{KeepAlivePeriod: 15 * time.Second, MaxIdleTimeout: time.Minute, MaxIncomingStreams: 64, MaxIncomingUniStreams: -1}
}
