package client

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/wire"
)

type RunOutput struct {
	Text      string
	Exit      int
	Alternate bool
}

func Result(conn *quic.Conn, id, run string, revision uint64) (RunOutput, error) {
	var result RunOutput
	stream, _, err := wire.Open(conn, wire.Message{Op: "result", ID: id, Run: run, Revision: revision})
	if err != nil {
		return result, err
	}
	defer stream.CancelRead(0)
	stream.Close()
	stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	var raw strings.Builder
	var truncated bool
	for {
		m, err := wire.Read(stream)
		if err != nil {
			return result, err
		}
		switch m.Op {
		case "data":
			if raw.Len()+len(m.Data) > 2<<20 {
				return result, errors.New("invalid run output")
			}
			raw.Write(m.Data)
		case "truncated":
			truncated = true
		case "exit":
			result.Exit = m.Code
			result.Alternate = m.Channel == "alternate"
			if !result.Alternate {
				result.Text = strings.Map(func(r rune) rune {
					if r == '\n' || r == '\t' || unicode.IsPrint(r) {
						return r
					}
					return -1
				}, ansi.Strip(raw.String()))
				if truncated {
					result.Text = "[Earlier output was discarded]\n" + result.Text
				}
			}
			return result, nil
		default:
			return result, errors.New("run output unavailable")
		}
	}
}
