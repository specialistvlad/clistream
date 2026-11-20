package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/wire"
)

func Files(c *quic.Conn, id, path string, visit func(wire.FileEntry) error) error {
	s, _, e := wire.Open(c, wire.Message{Op: "open", ID: id, Channel: "fs/list", Path: path})
	if e != nil {
		return e
	}
	defer s.CancelRead(0)
	s.Close()
	for {
		m, e := wire.Read(s)
		if e != nil {
			return e
		}
		if m.Op == "end" {
			return nil
		}
		if m.Op != "entry" || m.Entry == nil {
			return fmt.Errorf("listing failed: %s", m.Error)
		}
		if e = visit(*m.Entry); e != nil {
			return e
		}
	}
}
func Get(c *quic.Conn, id, path, dest string) error {
	s, _, e := wire.Open(c, wire.Message{Op: "open", ID: id, Channel: "fs/get", Path: path})
	if e != nil {
		return e
	}
	s.Close()
	defer s.CancelRead(0)
	f, e := os.CreateTemp(filepath.Dir(dest), ".clistream-download-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = files.Receive(s, f); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	return os.Link(f.Name(), dest)
}
func Put(c *quic.Conn, id, path, source string) error {
	f, e := os.Open(source)
	if e != nil {
		return e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil || !i.Mode().IsRegular() {
		return errors.New("upload a regular file")
	}
	s, _, e := wire.Open(c, wire.Message{Op: "open", ID: id, Channel: "fs/put", Path: path})
	if e != nil {
		return e
	}
	defer s.CancelRead(0)
	if e = files.Send(s, f); e != nil {
		wire.Abort(s)
		return e
	}
	s.Close()
	r, e := wire.Read(s)
	if e == nil && r.Op != "ok" {
		e = fmt.Errorf("upload failed: %s", r.Error)
	}
	return e
}
