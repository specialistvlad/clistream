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
	if directory {
		p = strings.TrimSuffix(p, "/")
	}
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
			f, err := r.open(m.Path, false)
			if err != nil {
				return e
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return err
			}
			if err = wire.Write(s, wire.Message{Op: "ok"}); err != nil {
				return err
			}
			if err = wire.Write(s, wire.Message{Op: "entry", Entry: &wire.FileEntry{Name: path.Base(m.Path), Size: info.Size(), Modified: info.ModTime().UnixNano()}}); err != nil {
				return err
			}
			return wire.Write(s, wire.Message{Op: "end"})
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
	case "fs/put", "fs/upload":
		dest := m.Path
		if m.Channel == "fs/upload" {
			if len(m.Args) != 1 || !valid(m.Args[0]) || path.Base(m.Args[0]) != m.Args[0] {
				return errors.New("invalid source name")
			}
			var err error
			dest, err = r.destination(dest, m.Args[0])
			if err != nil {
				return err
			}
		}
		err := r.writeFile(dest, m.Channel == "fs/upload", func(f *os.File) error {
			if err := wire.Write(s, wire.Message{Op: "ok"}); err != nil {
				return err
			}
			return Receive(s, f)
		})
		if err != nil {
			return err
		}
		return wire.Write(s, wire.Message{Op: "ok"})
	case "fs/cp", "fs/mv", "fs/rm", "fs/mkdir", "fs/rmdir":
		if err := wire.Write(s, wire.Message{Op: "ok"}); err != nil {
			return err
		}
		if err := r.change(m); err != nil {
			return err
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

func (r *Root) destination(dest, name string) (string, error) {
	requiresDir := strings.HasSuffix(dest, "/")
	dest = strings.TrimSuffix(dest, "/")
	if !valid(dest) {
		return "", errors.New("invalid destination")
	}
	if d, err := r.open(dest, true); err == nil {
		d.Close()
		dest = path.Join(dest, name)
	} else if requiresDir {
		return "", errors.New("destination must be a directory")
	}
	if f, err := r.open(dest, false); err == nil {
		f.Close()
	} else {
		dir, err := r.open(path.Dir(dest), true)
		if err != nil {
			return "", err
		}
		defer dir.Close()
		var st unix.Stat_t
		if err = unix.Fstatat(int(dir.Fd()), path.Base(dest), &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return "", errors.New("invalid destination")
		}
	}
	return dest, nil
}
func (r *Root) writeFile(dest string, replace bool, write func(*os.File) error) error {
	if !valid(dest) || dest == "." {
		return errors.New("invalid destination")
	}
	dir, e := r.open(path.Dir(dest), true)
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
	if e = write(f); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if replace {
		return unix.Renameat(fd, tmp, fd, path.Base(dest))
	}
	if e = unix.Linkat(fd, tmp, fd, path.Base(dest), 0); e != nil {
		return e
	}

	return nil
}
func (r *Root) change(m wire.Message) error {
	src := strings.TrimSuffix(m.Path, "/")
	requiresDir := src != m.Path
	if !valid(src) || src == "." {
		return errors.New("invalid workspace path")
	}
	parent, err := r.open(path.Dir(src), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	fd, name := int(parent.Fd()), path.Base(src)
	if m.Channel == "fs/mkdir" {
		return unix.Mkdirat(fd, name, 0700)
	}
	if requiresDir && (m.Channel == "fs/rm" || m.Channel == "fs/cp") {
		return errors.New("expected a regular file")
	}
	directory := m.Channel == "fs/rmdir" || requiresDir
	file, err := r.open(src, directory)
	if err != nil && m.Channel == "fs/mv" {
		file, err = r.open(src, true)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	switch m.Channel {
	case "fs/rm":
		return unix.Unlinkat(fd, name, 0)
	case "fs/rmdir":
		return unix.Unlinkat(fd, name, unix.AT_REMOVEDIR)
	}
	if len(m.Args) != 1 {
		return errors.New("expected destination")
	}
	dest, err := r.destination(m.Args[0], name)
	if err != nil {
		return err
	}
	if dest == src {
		return errors.New("source and destination are the same")
	}
	if m.Channel == "fs/cp" {
		return r.writeFile(dest, true, func(out *os.File) error { _, err := io.Copy(out, file); return err })
	}
	dir, err := r.open(path.Dir(dest), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Renameat(fd, name, int(dir.Fd()), path.Base(dest))
}
