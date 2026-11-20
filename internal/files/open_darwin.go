package files

import "golang.org/x/sys/unix"

func openChild(fd int, name string, flags int) (int, error) { return unix.Openat(fd, name, flags, 0) }
