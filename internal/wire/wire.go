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
	"slices"
	"time"

	"github.com/golang/snappy"
	"github.com/quic-go/quic-go"
)

const ALPN = "clistream/1+snappy"
const MaxPlain = 128 * 1024

type Session struct {
	ID, Name, Mode, Host, OS string
	Ports                    map[string]string
}
type FileEntry struct {
	Name           string
	Directory      bool
	Size, Modified int64
}

// Supervisor describes a controller and its currently attachable run.
type Supervisor struct {
	EditableCommand bool `json:",omitempty"`
	ID              string
	Info            Session
	Revision        uint64
	Exit            *int `json:",omitempty"`
	Failed          bool `json:",omitempty"`
}

const EditCommand = "edit-command-v1"
const ManagedSupervisors = "managed-supervisors-v1"

type Message struct {
	Command                   [][]byte     `json:",omitempty"`
	Capabilities              []string     `json:",omitempty"`
	Supervisor                *Supervisor  `json:",omitempty"`
	Supervisors               []Supervisor `json:",omitempty"`
	Revision                  uint64       `json:",omitempty"`
	Run                       string       `json:",omitempty"`
	Args                      []string     `json:",omitempty"`
	Path                      string       `json:",omitempty"`
	Entry                     *FileEntry   `json:",omitempty"`
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

// DetachCode closes an attachment stream while leaving its child running.
const DetachCode quic.StreamErrorCode = 2

func IsDetach(err error) bool {
	var reset *quic.StreamError
	return errors.As(err, &reset) && reset.ErrorCode == DetachCode
}

func Abort(s *quic.Stream) { s.CancelRead(1); s.CancelWrite(1) }
func Config() *quic.Config {
	return &quic.Config{KeepAlivePeriod: 15 * time.Second, MaxIdleTimeout: time.Minute, MaxIncomingStreams: 64, MaxIncomingUniStreams: -1}
}

// Request exchanges a single record on a short-lived stream.
func Request(c *quic.Conn, m Message) (Message, error) {
	st, res, err := Open(c, m)
	if err == nil {
		st.Close()
		st.CancelRead(0)
	}
	return res, err
}

// Managed probes using a record older strict decoders already understand.
func Managed(c *quic.Conn) (bool, error) {
	caps, err := Capabilities(c)
	return slices.Contains(caps, ManagedSupervisors), err
}
func Capabilities(c *quic.Conn) ([]string, error) {
	res, err := Request(c, Message{Op: "capabilities"})
	if res.Error == "unknown session or operation" || res.Error == "client authorization required" {
		return nil, nil
	}
	return res.Capabilities, err
}
