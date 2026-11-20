package wire

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/golang/snappy"
)

func TestRecords(t *testing.T) {
	for _, p := range [][]byte{{0}, bytes.Repeat([]byte("compiling src/main.go\n"), 1400)} {
		var b bytes.Buffer
		m := Message{Op: "data", Data: p}
		if err := Write(&b, m); err != nil {
			t.Fatal(err)
		}
		t.Logf("%d data bytes -> %d wire bytes", len(p), b.Len())
		got, e := Read(&b)
		if e != nil || !bytes.Equal(got.Data, p) {
			t.Fatal("round trip", e)
		}
	}
}
func TestReject(t *testing.T) {
	for _, body := range [][]byte{[]byte(`{"Op":"ok"}`), snappy.Encode(nil, make([]byte, MaxPlain+1)), snappy.Encode(nil, []byte(`{"Op":"ok","unknown":2}`))} {
		var b bytes.Buffer
		binary.Write(&b, binary.BigEndian, uint32(len(body)))
		b.Write(body)
		if _, e := Read(&b); e == nil {
			t.Fatal("invalid frame accepted")
		}
	}
}
