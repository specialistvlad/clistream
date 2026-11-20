// Package files confines file operations to a directory descriptor captured at startup.
package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/specialistvlad/clistream/internal/wire"
	"golang.org/x/sys/unix"
)

type Root struct {
	dir    *os.File
	device uint64
}

func Open(dir string) (*Root, error) {
	f, e := os.OpenFile(dir, os.O_RDONLY|unix.O_DIRECTORY, 0)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil || !i.IsDir() {
		f.Close()
		return nil, errors.New("invalid workspace root")
	}
	return &Root{f, uint64(i.Sys().(*syscall.Stat_t).Dev)}, nil
}
func (r *Root) Close() error { return r.dir.Close() }
func valid(p string) bool {
	if !fs.ValidPath(p) || len(p) > 4096 || strings.ContainsRune(p, 0) {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if strings.HasPrefix(c, ".clistream-upload-") {
			return false
		}
	}
	return true
}
func (r *Root) open(p string, directory bool) (*os.File, error) {
	if !valid(p) {
		return nil, errors.New("invalid workspace path")
	}
	fd, e := unix.Dup(int(r.dir.Fd()))
	if e != nil {
		return nil, e
	}
	components := strings.Split(p, "/")
	for i, part := range components {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if directory || i < len(components)-1 {
			flags |= unix.O_DIRECTORY
		} else {
			var st unix.Stat_t
			if err := unix.Fstatat(fd, part, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || uint64(st.Dev) != r.device {
				unix.Close(fd)
				return nil, errors.New("expected a workspace file")
			}
		}
		next, e := openChild(fd, part, flags)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil || uint64(st.Dev) != r.device {
			unix.Close(fd)
			return nil, errors.New("filesystem boundary")
		}
	}
	f := os.NewFile(uintptr(fd), p)
	i, e := f.Stat()
	if e != nil || (!directory && (!i.Mode().IsRegular() || i.Sys().(*syscall.Stat_t).Nlink != 1)) {
		f.Close()
		return nil, errors.New("expected a regular single-link file")
	}
	return f, nil
}
func (r *Root) Handle(s io.ReadWriter, m wire.Message) error {
	switch m.Channel {
	case "fs/list":
		dir, e := r.open(m.Path, true)
		if e != nil {
			return e
		}
		defer dir.Close()
		if e = wire.Write(s, wire.Message{Op: "ok"}); e != nil {
			return e
		}
		for {
			items, e := dir.ReadDir(64)
			for _, d := range items {
				if !valid(d.Name()) || (!d.IsDir() && !d.Type().IsRegular()) {
					continue
				}
				var st unix.Stat_t
				err := unix.Fstatat(int(dir.Fd()), d.Name(), &st, unix.AT_SYMLINK_NOFOLLOW)
				kind := st.Mode & unix.S_IFMT
				if err != nil || uint64(st.Dev) != r.device || (kind != unix.S_IFDIR && (kind != unix.S_IFREG || st.Nlink != 1)) {
					continue
				}
				if err = wire.Write(s, wire.Message{Op: "entry", Entry: &wire.FileEntry{Name: d.Name(), Directory: kind == unix.S_IFDIR, Size: st.Size, Modified: st.Mtim.Nano()}}); err != nil {
					return err
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		return wire.Write(s, wire.Message{Op: "end"})
	case "fs/get":
		f, e := r.open(m.Path, false)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = wire.Write(s, wire.Message{Op: "ok"}); e != nil {
			return e
		}
		return Send(s, f)
	case "fs/put":
		if !valid(m.Path) || m.Path == "." {
			return errors.New("invalid destination")
		}
		dir, e := r.open(path.Dir(m.Path), true)
		if e != nil {
			return e
		}
		defer dir.Close()
		fd := int(dir.Fd())
		var b [16]byte
		rand.Read(b[:])
		tmp := ".clistream-upload-" + hex.EncodeToString(b[:])
		n, e := unix.Openat(fd, tmp, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
		if e != nil {
			return e
		}
		f := os.NewFile(uintptr(n), tmp)
		defer f.Close()
		defer unix.Unlinkat(fd, tmp, 0)
		if e = wire.Write(s, wire.Message{Op: "ok"}); e != nil {
			return e
		}
		if e = Receive(s, f); e != nil {
			return e
		}
		if e = f.Sync(); e != nil {
			return e
		}
		if e = unix.Linkat(fd, tmp, fd, path.Base(m.Path), 0); e != nil {
			return e
		}
		return wire.Write(s, wire.Message{Op: "ok"})
	}
	return errors.New("unknown filesystem channel")
}
func Send(w io.Writer, r io.Reader) error {
	b := make([]byte, 32768)
	for {
		n, e := r.Read(b)
		if n > 0 {
			if err := wire.Write(w, wire.Message{Op: "data", Data: b[:n]}); err != nil {
				return err
			}
		}
		if e == io.EOF {
			return wire.Write(w, wire.Message{Op: "end"})
		}
		if e != nil {
			return e
		}
	}
}
func Receive(r io.Reader, w io.Writer) error {
	for {
		m, e := wire.Read(r)
		if e != nil {
			return e
		}
		switch m.Op {
		case "end":
			return nil
		case "data":
			if _, e = w.Write(m.Data); e != nil {
				return e
			}
		default:
			return fmt.Errorf("transfer failed: %s", m.Error)
		}
	}
}
