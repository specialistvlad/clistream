package files

import (
	"crypto/rand"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// Remove a directory as one visible mutation. Verify its whole tree before
// moving it out of the public namespace. A host-side crash or concurrent edit
// during cleanup can leave a private staging directory for the operator.
func (r *Root) removeTree(parent *os.File, name, p, version string) error {
	dir, err := r.openFrom(parent, name, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = r.walkRemoval(dir, false, len(p)); err != nil {
		return err
	}
	if _, _, err = r.checked(parent, name, version); err != nil {
		return err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	staged := ".clistream-upload-" + hex.EncodeToString(nonce[:])
	fd := int(parent.Fd())
	if err = renameExclusive(fd, name, fd, staged); err != nil {
		return err
	}
	// The rename committed the deletion. Unlink only entries that continue
	// to satisfy the workspace's descriptor and single-link restrictions.
	if err = r.walkRemoval(dir, true, len(p)); err == nil {
		_ = unix.Unlinkat(fd, staged, unix.AT_REMOVEDIR)
	}
	return nil
}
func (r *Root) walkRemoval(dir *os.File, remove bool, pathLen int) error {
	fd := int(dir.Fd())
	// Each pass needs an independent directory read offset.
	scanFD, err := openChild(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err != nil {
		return err
	}
	scan := os.NewFile(uintptr(scanFD), ".")
	defer scan.Close()
	for {
		items, err := scan.ReadDir(64)
		for _, item := range items {
			name := item.Name()
			length := pathLen + 1 + len(name)
			if !valid(name) || length > 4096 {
				return errInvalid
			}
			var st unix.Stat_t
			if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			isDir := st.Mode&unix.S_IFMT == unix.S_IFDIR
			if uint64(st.Dev) != r.device || (!isDir && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1)) {
				return errInvalid
			}
			flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
			unlinkFlags := 0
			if isDir {
				flags |= unix.O_DIRECTORY
				unlinkFlags = unix.AT_REMOVEDIR
			}
			entryFD, err := openChild(fd, name, flags)
			if err != nil {
				return err
			}
			entry := os.NewFile(uintptr(entryFD), name)
			var opened unix.Stat_t
			err = unix.Fstat(entryFD, &opened)
			if err == nil && (opened.Dev != st.Dev || opened.Ino != st.Ino || (!isDir && opened.Nlink != 1)) {
				err = errConflict
			}
			if err == nil && isDir {
				err = r.walkRemoval(entry, remove, length)
			}
			entry.Close()
			if err != nil {
				return err
			}
			if remove {
				if err = unix.Unlinkat(fd, name, unlinkFlags); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
