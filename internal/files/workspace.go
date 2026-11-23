// The replicated workspace protocol uses relative paths for routing, and
// opaque versions for compare-and-swap. Legacy file channels stay unchanged.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/specialistvlad/clistream/internal/wire"
	"golang.org/x/sys/unix"
)

var (
	errConflict    = errors.New("the remote item changed")
	errInvalid     = errors.New("invalid workspace request")
	errUnavailable = errors.New("workspace unavailable")
)

func workspaceError(err error) wire.Message {
	kind := "invalid"
	switch {
	case errors.Is(err, errConflict):
		kind = "conflict"
	case errors.Is(err, unix.EEXIST):
		kind = "exists"
	case errors.Is(err, unix.ENOENT):
		kind = "missing"
	case errors.Is(err, unix.ENOTEMPTY):
		kind = "notEmpty"
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM), errors.Is(err, unix.EROFS):
		kind = "permission"
	case errors.Is(err, unix.ENOSPC), errors.Is(err, unix.EDQUOT):
		kind = "noSpace"
	case errors.Is(err, errUnavailable), errors.Is(err, unix.EIO):
		kind = "unavailable"
	}
	return wire.Message{Op: "error", Error: err.Error(), ErrorKind: kind}
}

func (r *Root) openItem(parent *os.File, p string) (*os.File, error) {
	f, err := r.openFrom(parent, p, true)
	if !errors.Is(err, unix.ENOTDIR) {
		return f, err
	}
	return r.openFrom(parent, p, false)
}

// snapshot holds a descriptor while reading the attributes, so its identifier
// and version describe the same object even if a host process renames it.
func (r *Root) snapshot(f *os.File, name string) (wire.FileEntry, string, error) {
	var st unix.Stat_t
	fd := int(f.Fd())
	if err := unix.Fstat(fd, &st); err != nil {
		return wire.FileEntry{}, "", err
	}
	key := fmt.Sprintf("%x-%x-%x", st.Dev, st.Ino, creationTime(fd, &st))
	id := r.aliases[key]
	if id == "" {
		id = key
	}
	v := fmt.Sprintf("%s-%x-%x-%x", key, st.Ctim.Nano(), st.Mtim.Nano(), st.Size)
	return wire.FileEntry{ID: id, Name: name, Directory: st.Mode&unix.S_IFMT == unix.S_IFDIR, Size: st.Size, Modified: st.Mtim.Nano(), Version: v}, key, nil
}

func (r *Root) inspect(p string) (wire.FileEntry, string, error) {
	return r.inspectAt(r.dir, p)
}

func (r *Root) inspectAt(parent *os.File, p string) (wire.FileEntry, string, error) {
	f, err := r.openItem(parent, p)
	if err != nil {
		return wire.FileEntry{}, "", err
	}
	defer f.Close()
	return r.snapshot(f, path.Base(p))
}

func (r *Root) checked(parent *os.File, p, version string) (wire.FileEntry, string, error) {
	item, key, err := r.inspectAt(parent, p)
	if err == nil && item.Version != version {
		err = errConflict
	}
	return item, key, err
}

func (r *Root) ServeWorkspace(s io.ReadWriter, m wire.Message) error {
	// Send the first acknowledgement only after validating any prerequisites
	// that might reject a streaming upload. Every accepted mutation returns
	// another acknowledgement after the host filesystem commits it.
	err := r.workspace(s, m)
	if err != nil {
		_ = wire.Write(s, workspaceError(err))
	}
	return err
}

func (r *Root) workspace(s io.ReadWriter, m wire.Message) error {
	if r == nil {
		return errUnavailable
	}
	if !valid(m.Path) || !wire.WorkspaceChannel(m.Channel) {
		return errInvalid
	}
	switch m.Channel {
	case "workspace/list":
		return r.listWorkspace(s, m.Path)
	case "workspace/fetch":
		return r.fetch(s, m)
	case "workspace/stat":
		r.mu.Lock()
		entry, _, err := r.inspect(m.Path)
		r.mu.Unlock()
		if err != nil {
			return err
		}
		return wire.Write(s, wire.Message{Op: "ok", Entry: &entry})
	}
	if m.Path == "." {
		return errInvalid
	}
	if m.Channel == "workspace/replace" || m.Channel == "workspace/move" || m.Channel == "workspace/remove" {
		if m.Version == "" {
			return errInvalid
		}
	}
	if m.Channel == "workspace/create" || m.Channel == "workspace/replace" {
		return r.store(s, m)
	}
	if err := wire.Write(s, wire.Message{Op: "ok"}); err != nil {
		return err
	}
	r.mu.Lock()
	entry, err := r.changeWorkspace(m)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return wire.Write(s, wire.Message{Op: "ok", Entry: entry})
}

func (r *Root) listWorkspace(s io.Writer, p string) error {
	dir, err := r.open(p, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = wire.Write(s, wire.Message{Op: "ok"}); err != nil {
		return err
	}
	for {
		entries, err := dir.ReadDir(64)
		for _, v := range entries {
			if !valid(v.Name()) || (!v.IsDir() && !v.Type().IsRegular()) {
				continue
			}
			r.mu.Lock()
			item, _, lookupErr := r.inspectAt(dir, v.Name())
			r.mu.Unlock()
			// Concurrent removals and disallowed entries are omitted.
			if lookupErr != nil {
				continue
			}
			if err := wire.Write(s, wire.Message{Op: "entry", Entry: &item}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return wire.Write(s, wire.Message{Op: "end"})
		}
		if err != nil {
			return err
		}
	}
}

func (r *Root) fetch(s io.Writer, m wire.Message) error {
	f, err := r.open(m.Path, false)
	if err != nil {
		return err
	}
	defer f.Close()
	r.mu.Lock()
	before, _, err := r.snapshot(f, path.Base(m.Path))
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if m.Version != "" && m.Version != before.Version {
		return errConflict
	}
	if err = wire.Write(s, wire.Message{Op: "ok", Entry: &before}); err != nil {
		return err
	}
	if err = sendData(s, f); err != nil {
		return err
	}
	// A native program may change the file in place while streaming its bytes.
	r.mu.Lock()
	_, _, err = r.checked(r.dir, m.Path, before.Version)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return wire.Write(s, wire.Message{Op: "end"})
}

func (r *Root) store(s io.ReadWriter, m wire.Message) error {
	dir, err := r.open(path.Dir(m.Path), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, name := int(dir.Fd()), path.Base(m.Path)
	replacing := m.Channel == "workspace/replace"
	checkDestination := func() (wire.FileEntry, string, error) {
		if replacing {
			item, key, err := r.checked(dir, name, m.Version)
			if err == nil && item.Directory {
				err = errInvalid
			}
			return item, key, err
		}
		var st unix.Stat_t
		err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil {
			err = unix.EEXIST
		} else if errors.Is(err, unix.ENOENT) {
			err = nil
		}
		return wire.FileEntry{}, "", err
	}
	r.mu.Lock()
	_, _, err = checkDestination()
	r.mu.Unlock()
	if err != nil {
		return err
	}
	var item wire.FileEntry
	err = stageFile(dir, func(f *os.File) error {
		if err := wire.Write(s, wire.Message{Op: "ok"}); err != nil {
			return err
		}
		return Receive(s, f)
	}, func(tmp string, staged *os.File) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		current, err := r.open(path.Dir(m.Path), true)
		if err != nil {
			return err
		}
		defer current.Close()
		a, err := dir.Stat()
		if err != nil {
			return err
		}
		b, err := current.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(a, b) {
			return errConflict
		}
		old, oldKey, err := checkDestination()
		if err != nil {
			return err
		}
		if replacing {
			// Keep executable and ordinary permission bits when Finder saves an edit.
			var prior unix.Stat_t
			if err = unix.Fstatat(fd, name, &prior, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if err = staged.Chmod(os.FileMode(prior.Mode & 0777)); err != nil {
				return err
			}
			err = unix.Renameat(fd, tmp, fd, name)
		} else {
			err = renameExclusive(fd, tmp, fd, name)
		}
		if err != nil {
			return err
		}
		var key string
		item, key, err = r.snapshot(staged, name)
		if err == nil && replacing {
			delete(r.aliases, oldKey)
			r.aliases[key] = old.ID
			item.ID = old.ID
		}
		return err
	})
	if err != nil {
		return err
	}
	return wire.Write(s, wire.Message{Op: "ok", Entry: &item})
}

func (r *Root) changeWorkspace(m wire.Message) (*wire.FileEntry, error) {
	parent, err := r.open(path.Dir(m.Path), true)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, name := int(parent.Fd()), path.Base(m.Path)
	target := m.Path
	switch m.Channel {
	case "workspace/mkdir":
		err = unix.Mkdirat(fd, name, 0700)
	case "workspace/move", "workspace/remove":
		item, key, err := r.checked(parent, name, m.Version)
		if err != nil {
			return nil, err
		}
		if m.Channel == "workspace/remove" {
			if item.Directory && m.Recursive {
				return nil, r.removeTree(parent, name, m.Path, m.Version)
			}
			flags := 0
			if item.Directory {
				flags = unix.AT_REMOVEDIR
			}
			err = unix.Unlinkat(fd, name, flags)
			if err == nil {
				delete(r.aliases, key)
			}
			return nil, err
		}
		if len(m.Args) != 1 || !valid(m.Args[0]) || m.Args[0] == "." {
			return nil, errInvalid
		}
		target = m.Args[0]
		dest, err := r.open(path.Dir(target), true)
		if err != nil {
			return nil, err
		}
		defer dest.Close()
		if target != m.Path {
			if err = renameExclusive(fd, name, int(dest.Fd()), path.Base(target)); errors.Is(err, unix.EEXIST) {
				// A case-only rename can name the same directory entry on APFS.
				current, _, lookup := r.inspectAt(dest, path.Base(target))
				if lookup == nil && current.Version == item.Version {
					err = unix.Renameat(fd, name, int(dest.Fd()), path.Base(target))
				}
			}
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, errInvalid
	}
	if err != nil {
		return nil, err
	}
	entry, _, err := r.inspect(target)
	return &entry, err
}
