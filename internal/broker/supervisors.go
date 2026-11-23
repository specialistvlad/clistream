package broker

import (
	"errors"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

// publish updates both registries under the broker lock so legacy listings
// immediately reflect the same attachable run as the management dashboard.
func (s *Server) publish(c *quic.Conn, v *wire.Supervisor) error {
	if v == nil || !validID(v.ID) || v.Revision == 0 || (v.Info.Mode != "pipe" && v.Info.Mode != "pty") {
		return errors.New("invalid supervisor")
	}
	old, exists := s.controllers[v.ID]
	if exists && (old.conn != c || old.state.Revision >= v.Revision) {
		return errors.New("stale supervisor")
	}
	for key, other := range s.controllers {
		if other.conn == c && key != v.ID {
			return errors.New("already registered")
		}
	}
	if !exists {
		for _, entry := range s.sessions {
			if entry.conn == c {
				return errors.New("already registered")
			}
		}
	}
	run := v.Info.ID
	if run != "" {
		if !validID(run) {
			return errors.New("invalid run")
		}
		if current, ok := s.sessions[run]; ok && current.conn != c {
			return errors.New("duplicate run")
		}
	}
	if exists {
		delete(s.sessions, old.state.Info.ID)
	}
	if run != "" {
		s.sessions[run] = entry{info: v.Info, conn: c}
	}
	s.controllers[v.ID] = managedEntry{*v, c}
	return nil
}

func (s *Server) supervisors() []wire.Supervisor {
	var rows []wire.Supervisor
	managed := map[string]bool{}
	for _, v := range s.controllers {
		rows = append(rows, v.state)
		managed[v.state.Info.ID] = true
	}
	for id, v := range s.sessions {
		if !managed[id] {
			rows = append(rows, wire.Supervisor{ID: id, Info: v.info, Workspace: v.workspace})
		}
	}
	return rows
}
