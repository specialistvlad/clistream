// Package commandline handles the printable CLIStream executable and argument syntax.
package commandline

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Quote control characters and arbitrary Unix argument bytes into one printable line.
func Format(argv [][]byte) string {
	parts := make([]string, len(argv))
	for i, b := range argv {
		s := string(b)
		if s == "" || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool {
			return !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune("'\"\\", r)
		}) >= 0 {
			s = strconv.Quote(s)
		}
		parts[i] = s
	}
	return strings.Join(parts, " ")
}

func Parse(line string) ([][]byte, error) {
	var argv [][]byte
	var word strings.Builder
	var quote rune
	started := false
	for line != "" {
		r, n := utf8.DecodeRuneInString(line)
		line = line[n:]
		switch {
		case quote == 0 && unicode.IsSpace(r):
			if started {
				argv = append(argv, []byte(word.String()))
				word.Reset()
				started = false
			}
			continue
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case r == '\\' && quote != '\'':
			if line == "" {
				return nil, errors.New("Complete the trailing escape.")
			}
			if quote == '"' {
				value, multi, rest, err := strconv.UnquoteChar("\\"+line, '"')
				if err != nil {
					return nil, errors.New("Invalid escape in double quotes.")
				}
				if multi {
					word.WriteRune(value)
				} else {
					word.WriteByte(byte(value))
				}
				line = rest
			} else {
				v, size := utf8.DecodeRuneInString(line)
				word.WriteRune(v)
				line = line[size:]
			}
		default:
			word.WriteRune(r)
		}
		started = true
	}
	if quote != 0 {
		return nil, errors.New("Close the unfinished quote.")
	}
	if started {
		argv = append(argv, []byte(word.String()))
	}
	if len(argv) == 0 || len(argv[0]) == 0 {
		return nil, errors.New("Enter an executable.")
	}
	return argv, nil
}
