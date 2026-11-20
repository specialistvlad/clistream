// Command dist builds deterministic release archives and verifies a native archive.
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var members = []string{"LICENSE", "README.md", "clistream", "clistream-supervise", "clistreamd"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 3 && os.Args[1] == "verify" {
		return verify(os.Args[2])
	}
	if len(os.Args) != 1 {
		return fmt.Errorf("usage: dist [verify ARCHIVE]")
	}
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	var manifest strings.Builder
	for _, system := range []string{"darwin", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := "clistream-" + system + "-" + arch
			path := filepath.Join("dist", name+".tar.gz")
			if err := build(system, arch, name, path); err != nil {
				return err
			}
			digest, err := checksum(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&manifest, "%s  %s\n", digest, filepath.Base(path))
		}
	}
	return os.WriteFile("dist/SHA256SUMS", []byte(manifest.String()), 0644)
}

func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), err
}

func build(system, arch, name, path string) error {
	stage, err := os.MkdirTemp("", "clistream-dist")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", stage+string(os.PathSeparator), "./cmd/...")
	cmd.Env = append(os.Environ(), "GOOS="+system, "GOARCH="+arch, "CGO_ENABLED=0")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	f, err := os.CreateTemp("dist", ".archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, member := range members {
		source := member
		mode := int64(0644)
		if strings.HasPrefix(member, "clistream") {
			source = filepath.Join(stage, member)
			mode = 0755
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name + "/" + member, Mode: mode, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func verify(path string) error {
	sum, err := checksum(path)
	if err != nil {
		return err
	}
	manifest, err := os.Open(filepath.Join(filepath.Dir(path), "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer manifest.Close()
	scan := bufio.NewScanner(manifest)
	matched := false
	for scan.Scan() {
		if scan.Text() == sum+"  "+filepath.Base(path) {
			matched = true
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("checksum mismatch for %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	stage, err := os.MkdirTemp("", "clistream-verify")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	root := strings.TrimSuffix(filepath.Base(path), ".tar.gz")
	tr := tar.NewReader(gz)
	for _, member := range members {
		h, err := tr.Next()
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(member, "clistream") {
			mode = 0755
		}
		if h.Name != root+"/"+member || h.Typeflag != tar.TypeReg || h.Mode != int64(mode) {
			return fmt.Errorf("unexpected archive entry %q", h.Name)
		}
		dst, err := os.OpenFile(filepath.Join(stage, member), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, tr)
		closeErr := dst.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if _, err = tr.Next(); err != io.EOF {
		return fmt.Errorf("unexpected trailing archive entry")
	}
	actual, err := checksum(filepath.Join(stage, "LICENSE"))
	if err != nil {
		return err
	}
	want, err := checksum("LICENSE")
	if err != nil {
		return err
	}
	if actual != want {
		return fmt.Errorf("packaged license differs")
	}
	cmd := exec.Command("go", "test", "-tags=e2e", "-count=1", "-timeout=15m", "./tests/e2e")
	cmd.Env = append(os.Environ(), "CLISTREAM_BIN="+stage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
