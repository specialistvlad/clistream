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
		if arg == "-h" || arg == "--help" {
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

func PrintDefaults() {
	fs := flag.CommandLine
	fmt.Fprintln(fs.Output(), "  -h, --help\n      show command help")
	fs.VisitAll(func(f *flag.Flag) {
		metavar, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(fs.Output(), "  --%s", f.Name)
		if metavar != "" {
			fmt.Fprintf(fs.Output(), " %s", metavar)
		}
		fmt.Fprintf(fs.Output(), "\n      %s", usage)
		if f.DefValue != "" {
			fmt.Fprintf(fs.Output(), " (default %s)", f.DefValue)
		}
		fmt.Fprintln(fs.Output())
	})
}
