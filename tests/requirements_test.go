package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRequirementsTrace(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	tests := map[string]bool{}
	for _, dir := range []string{"cmd", "internal", "tests"} {
		err := filepath.WalkDir(filepath.Join("..", dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
					tests[fn.Name.Name] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	swift, err := filepath.Glob("../apple/*Tests/*.swift")
	if err != nil {
		t.Fatal(err)
	}
	swiftCase := regexp.MustCompile(`\bfunc (test\w+)\(`)
	for _, path := range swift {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range swiftCase.FindAllStringSubmatch(string(data), -1) {
			tests[m[1]] = true
		}
	}
	for _, m := range regexp.MustCompile(`(?m)^([\w-]+):`).FindAllStringSubmatch(read("Makefile"), -1) {
		tests["make "+m[1]] = true
	}
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\| ([FN]\d+) \|`).FindAllStringSubmatch(read("docs/requirements.md"), -1) {
		if _, ok := ids[m[1]]; ok {
			t.Fatal("duplicate", m[1])
		}
		ids[m[1]] = false
	}
	rowID := regexp.MustCompile(`^\| ([FN]\d+)\.`)
	ref := regexp.MustCompile("\x60([^\x60]+)\x60")
	for line := range strings.SplitSeq(read("docs/test-results/requirements-audit.md"), "\n") {
		m := rowID.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if covered, ok := ids[m[1]]; !ok || covered {
			t.Fatal("unexpected audit row", m[1])
		}
		ids[m[1]] = true
		cols := strings.Split(line, "|")
		if len(cols) < 4 {
			t.Fatal("invalid row", line)
		}
		refs := ref.FindAllStringSubmatch(cols[2], -1)
		if len(refs) == 0 {
			t.Fatal("missing tests", m[1])
		}
		for _, r := range refs {
			if tests[r[1]] {
				continue
			}
			if strings.HasPrefix(r[1], "apple/") {
				if info, err := os.Stat(filepath.Join("..", r[1])); err == nil && info.Mode().IsRegular() {
					continue
				}
			}
			t.Errorf("%s references unknown test %s", m[1], r[1])
		}
	}
	if len(ids) == 0 {
		t.Fatal("no requirements found")
	}
	for id, covered := range ids {
		if !covered {
			t.Error("missing audit row", id)
		}
	}
}
