// Package cliopts parses the common long-option syntax of the CLIStream commands.
package cliopts

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func Parse() {
	fs := flag.CommandLine
	err := parse(fs, os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		flag.Usage()
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(fs.Output(), err)
		flag.Usage()
		os.Exit(2)
	}
}

func parse(fs *flag.FlagSet, args []string) error {
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			args = args[1:]
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			break
		}
		if arg == "--help" {
			return flag.ErrHelp
		}
		name, value, inline := strings.Cut(arg, "=")
		if !strings.HasPrefix(name, "--") {
			return fmt.Errorf("unknown option %s (long options require --)", name)
		}
		name = name[2:]
		opt := fs.Lookup(name)
		if opt == nil {
			return fmt.Errorf("unknown option --%s", name)
		}
		args = args[1:]
		if !inline {
			if b, ok := opt.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				value = "true"
			} else if len(args) == 0 {
				return fmt.Errorf("--%s requires a value", name)
			} else {
				value, args = args[0], args[1:]
			}
		}
		if err := fs.Set(name, value); err != nil {
			return fmt.Errorf("invalid value for --%s: %w", name, err)
		}
	}
	return fs.Parse(append([]string{"--"}, args...))
}

// Supplied reports whether an option was explicitly provided, even at its default value.
func Supplied(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

type Command struct {
	Arguments string
	Min, Max  int
}

// Validate checks operands before a command reads credentials or connects.
func Validate(program string, args []string, commands map[string]Command) error {
	command, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q; see %s --help", args[0], program)
	}
	count := len(args) - 1
	if count < command.Min || count > command.Max {
		return fmt.Errorf("usage: %s %s", program, strings.TrimSpace(args[0]+" "+command.Arguments))
	}
	return nil
}

type Section struct{ Title, Body string }

func Help(purpose, usage string, sections ...Section) {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, "%s\n\nUsage: %s\n", purpose, usage)
	for _, section := range sections {
		fmt.Fprintf(out, "\n%s\n%s\n%s\n", section.Title, strings.Repeat("-", len(section.Title)), section.Body)
	}
}
