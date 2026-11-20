package files

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// openChild receives exactly one validated path component. Emulators and older
// kernels may lack openat2. In that case keep the no-follow descriptor walk and
// verify kernel mount IDs, including for bind mounts on the same filesystem.
func openChild(parent int, name string, flags int) (int, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{
		Flags: uint64(flags), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
	})
	if !errors.Is(err, unix.ENOSYS) {
		return fd, err
	}
	return openCheckedMount(parent, name, flags)
}

func openCheckedMount(parent int, name string, flags int) (int, error) {
	fd, err := unix.Openat(parent, name, flags|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	parentMount, err := mountID(parent)
	if err == nil {
		var childMount uint64
		childMount, err = mountID(fd)
		if err == nil && parentMount != childMount {
			err = unix.EXDEV
		}
	}
	if err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func mountID(fd int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if id, ok := strings.CutPrefix(line, "mnt_id:"); ok {
			return strconv.ParseUint(strings.TrimSpace(id), 10, 64)
		}
	}
	return 0, errors.New("kernel mount ID unavailable")
}
