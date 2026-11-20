package files

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFallbackRejectsMountCrossing(t *testing.T) {
	root, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	fd, err := openCheckedMount(int(root.Fd()), "proc", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err == nil {
		unix.Close(fd)
		t.Fatal("crossed procfs mount")
	}
	if !errors.Is(err, unix.EXDEV) {
		t.Fatalf("expected mount boundary error, got %v", err)
	}
}
