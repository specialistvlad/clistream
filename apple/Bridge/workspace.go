package main

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/wire"
)

// Each in-flight File Provider request has its own short-lived connection so
// Finder can cancel a copy without disturbing a terminal or another transfer.
var workspaceTasks sync.Map

func workspaceCall(q request) (response, error) {
	if q.Op == "workspace-cancel" {
		if stop, ok := workspaceTasks.Load(q.Handle); ok {
			stop.(context.CancelFunc)()
		}
		return response{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if q.Handle != "" {
		if _, exists := workspaceTasks.LoadOrStore(q.Handle, cancel); exists {
			return response{ErrorKind: "invalid"}, errors.New("duplicate transfer handle")
		}
		defer workspaceTasks.Delete(q.Handle)
	}
	c, err := client.Connect(q.Dir)
	if err != nil {
		return response{ErrorKind: "unavailable"}, err
	}
	defer c.CloseWithError(0, "")
	stop := context.AfterFunc(ctx, func() { c.CloseWithError(0, "transfer cancelled") })
	defer stop()
	m := wire.Message{Op: "workspace", Channel: "workspace/" + strings.TrimPrefix(q.Op, "workspace-"), Path: q.Path, ID: q.ID, Version: q.Version, Recursive: q.Recursive}
	if q.Op == "workspace-move" {
		m.Args = []string{q.Destination}
	}
	var r response
	var entries = []wire.FileEntry{}
	r.Entry, err = client.Workspace(c, m, q.LocalPath, func(entry wire.FileEntry) error { entries = append(entries, entry); return nil })
	if err != nil {
		r.ErrorKind = "unavailable"
		var remote *wire.RemoteError
		var transport *quic.ApplicationError
		if errors.As(err, &remote) {
			if remote.Kind != "" {
				r.ErrorKind = remote.Kind
			}
			if remote.Message == "client authorization required" {
				r.ErrorKind = "denied"
			}
		}
		if errors.As(err, &transport) && transport.ErrorMessage == "revoked" {
			r.ErrorKind = "denied"
		}
		return r, err
	}
	if q.Op == "workspace-list" {
		r.Entries = &entries
	}
	return r, nil
}
