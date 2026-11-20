package main

import (
	"errors"
	"fmt"
	"golang.org/x/term"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/quic-go/quic-go"
	"github.com/specialistvlad/clistream/internal/client"
	"github.com/specialistvlad/clistream/internal/cliopts"
	"github.com/specialistvlad/clistream/internal/wire"
)

var fsCommands = map[string]cliopts.Command{
	"ls":    {Arguments: "[--all] [PATH]", Max: 2},
	"cp":    {Arguments: "SOURCE DEST", Min: 2, Max: 2},
	"mv":    {Arguments: "SOURCE DEST", Min: 2, Max: 2},
	"rm":    {Arguments: "FILE", Min: 1, Max: 1},
	"mkdir": {Arguments: "DIR", Min: 1, Max: 1},
	"rmdir": {Arguments: "DIR", Min: 1, Max: 1},
}

func validateFS(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: clistream SESSION_ID fs COMMAND [PATH ...]")
	}
	if args[0] == "ls" && len(args) == 3 && args[1] != "--all" {
		return errors.New("usage: clistream SESSION_ID fs ls [--all] [PATH]")
	}
	return cliopts.Validate("clistream SESSION_ID fs", args, fsCommands)
}

// Normalize dot components while preserving a traversal attempt for rejection by the host.
func sharedPath(p string) string {
	if p == "" || strings.HasPrefix(p, "/") || slices.Contains(strings.Split(p, "/"), "..") {
		return p
	}
	clean := path.Clean(p)
	if clean != "." && (strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/.")) {
		return clean + "/"
	}
	return clean
}
func runFS(c *quic.Conn, id string, args []string) error {
	op := args[0]
	if op == "ls" {
		dir := "."
		all := len(args) > 1 && args[1] == "--all"
		paths := args[1:]
		if all {
			paths = paths[1:]
		}
		if len(paths) > 0 {
			dir = sharedPath(paths[0])
		}
		var names []string
		err := client.Files(c, id, dir, func(entry wire.FileEntry) error {
			if all || entry.Name == path.Base(dir) || !strings.HasPrefix(entry.Name, ".") {
				names = append(names, entry.Name)
			}
			return nil
		})
		if err != nil {
			return err
		}
		slices.Sort(names)
		for _, name := range names {
			if term.IsTerminal(int(os.Stdout.Fd())) && strings.IndexFunc(name, unicode.IsControl) >= 0 {
				name = strconv.Quote(name)
			}
			if _, err := fmt.Fprintln(os.Stdout, name); err != nil {
				return err
			}
		}
		return nil
	}
	src := args[1]
	m := wire.Message{Op: "open", ID: id, Channel: "fs/" + op, Path: sharedPath(src)}
	if len(args) > 2 {
		dest := args[2]
		if op == "cp" {
			localSrc, upload := strings.CutPrefix(src, "local:")
			localDest, download := strings.CutPrefix(dest, "local:")
			if upload && download {
				return errors.New("cp requires a shared source or destination")
			}
			if upload {
				return client.Upload(c, id, sharedPath(dest), localSrc)
			}
			if download {
				if info, err := os.Stat(localDest); err == nil && info.IsDir() {
					localDest = filepath.Join(localDest, path.Base(src))
				}
				return client.Download(c, id, sharedPath(src), localDest)
			}
		}
		m.Args = []string{sharedPath(dest)}
	}
	stream, _, err := wire.Open(c, m)
	if err != nil {
		return err
	}
	defer stream.CancelRead(0)
	stream.Close()
	reply, err := wire.Read(stream)
	if err != nil {
		return err
	}
	if reply.Op != "ok" {
		return fmt.Errorf("%s failed: %s", op, reply.Error)
	}
	return nil
}
