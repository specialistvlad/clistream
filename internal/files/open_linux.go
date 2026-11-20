package files

import "golang.org/x/sys/unix"

func openChild(fd int, name string, flags int) (int, error) {
	return unix.Openat2(fd, name, &unix.OpenHow{Flags: uint64(flags), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
}
