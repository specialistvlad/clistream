package client

import (
	"errors"
	"os"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/files"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Workspace performs a negotiated operation on a supervisor's exported root.
// File contents stream directly between the supplied local file and QUIC.
func Workspace(c *quic.Conn, m wire.Message, localPath string, visit func(wire.FileEntry) error) (*wire.FileEntry, error) {
	if m.Op != "workspace" || !wire.WorkspaceChannel(m.Channel) {
		return nil, errors.New("unknown workspace operation")
	}
	var source *os.File
	if m.Channel == "workspace/create" || m.Channel == "workspace/replace" {
		var err error
		source, err = os.Open(localPath)
		if err != nil {
			return nil, err
		}
		defer source.Close()
		info, err := source.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("expected a regular file")
		}
	}
	stream, reply, err := wire.Open(c, m)
	if err != nil {
		return nil, err
	}
	defer stream.CancelRead(0)
	if source != nil {
		if err = files.Send(stream, source); err != nil {
			wire.Abort(stream)
			return nil, err
		}
	}
	stream.Close()
	switch m.Channel {
	case "workspace/create", "workspace/replace", "workspace/mkdir", "workspace/move", "workspace/remove":
		reply, err = wire.Read(stream)
		if err != nil {
			return nil, err
		}
		if reply.Op == "error" {
			return nil, reply.Rejection()
		}
		if reply.Op != "ok" {
			return nil, errors.New("invalid workspace acknowledgement")
		}
	}
	switch m.Channel {
	case "workspace/list":
		for {
			record, err := wire.Read(stream)
			if err != nil {
				return nil, err
			}
			switch record.Op {
			case "end":
				return nil, nil
			case "error":
				return nil, record.Rejection()
			case "entry":
				if record.Entry == nil || record.Entry.ID == "" || record.Entry.Version == "" {
					return nil, errors.New("invalid workspace entry")
				}
				if visit != nil {
					if err = visit(*record.Entry); err != nil {
						return nil, err
					}
				}
			default:
				return nil, errors.New("invalid workspace listing")
			}
		}
	case "workspace/remove":
		return nil, nil
	}
	if reply.Entry == nil || reply.Entry.ID == "" || reply.Entry.Version == "" {
		return nil, errors.New("missing workspace entry")
	}
	if m.Channel == "workspace/fetch" {
		if err = receiveFile(stream, localPath, true); err != nil {
			return nil, err
		}
	}
	return reply.Entry, nil
}
