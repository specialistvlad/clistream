package client

import (
	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

func Supervisors(c *quic.Conn, managed, editor, result bool) ([]wire.Supervisor, error) {
	var rows []wire.Supervisor
	if managed {
		op := "supervisors"
		if editor {
			op = "command-supervisors"
		}
		if result {
			op = "result-supervisors"
		}
		res, err := wire.Request(c, wire.Message{Op: op})
		if err != nil {
			return nil, err
		}
		rows = res.Supervisors
	} else {
		items, err := List(c)
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			rows = append(rows, wire.Supervisor{ID: v.ID, Info: v})
		}
	}
	return rows, nil
}
