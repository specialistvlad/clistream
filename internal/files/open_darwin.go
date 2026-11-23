package files

import "golang.org/x/sys/unix"

func openChild(fd int, name string, flags int) (int, error) { return unix.Openat(fd, name, flags, 0) }

func creationTime(_ int, st *unix.Stat_t) int64 { return st.Btim.Nano() }

func renameExclusive(fromFD int, from string, toFD int, to string) error {
	return unix.RenameatxNp(fromFD, from, toFD, to, unix.RENAME_EXCL)
}
