package files

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/specialistvlad/clistream/internal/wire"
)

type duplex struct {
	*bytes.Reader
	output bytes.Buffer
}

func (d *duplex) Write(b []byte) (int, error) { return d.output.Write(b) }
func TestPartialUploadIsRemoved(t *testing.T) {
	dir := t.TempDir()
	r, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	var in bytes.Buffer
	wire.Write(&in, wire.Message{Op: "data", Data: []byte("partial")})
	s := &duplex{Reader: bytes.NewReader(in.Bytes())}
	if e = r.Handle(s, wire.Message{Channel: "fs/put", Path: "result"}); e == nil {
		t.Fatal("partial upload accepted")
	}
	items, e := os.ReadDir(dir)
	if e != nil || len(items) != 0 {
		t.Fatal("staging artifact", items, e)
	}
}
func TestRootFollowsOriginalDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "work")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	r, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if e = os.Rename(dir, dir+"-moved"); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(parent, dir); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(parent, "outside"), []byte("private"), 0600); e != nil {
		t.Fatal(e)
	}
	if f, e := r.open("outside", false); e == nil {
		f.Close()
		t.Fatal("root replaced")
	}
}
