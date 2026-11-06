package codestyle

import (
	"bufio"
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var goDirective = regexp.MustCompile(`^//(go:(build|embed|generate)|lint:ignore )`)

var skippedDirs = map[string]bool{
	"node_modules":      true,
	"dist":              true,
	"testdata":          true,
	"playwright-report": true,
	"test-results":      true,
}

const lastCommentedMigration = 8

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func sourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") && name != ".github" || skippedDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSourcesCarryNoComments(t *testing.T) {
	root := moduleRoot(t)
	var found []string
	for _, path := range sourceFiles(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := commentLines(path, rel)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, line := range lines {
			found = append(found, fmt.Sprintf("%s:%d", rel, line))
		}
	}
	if len(found) > 0 {
		t.Fatalf("code comments are not allowed; remove them:\n%s", strings.Join(found, "\n"))
	}
}

func commentLines(path, rel string) ([]int, error) {
	name := filepath.Base(path)
	switch {
	case strings.HasSuffix(name, ".go"):
		return goComments(path)
	case strings.HasSuffix(name, ".sql"):
		if migrationVersion(name) <= lastCommentedMigration && strings.HasPrefix(rel, "migrations"+string(filepath.Separator)) {
			return nil, nil
		}
		return prefixedLines(path, "--", nil)
	case strings.HasSuffix(name, ".sh"), strings.HasSuffix(name, ".tftpl"):
		return prefixedLines(path, "#", []string{"#!", "# shellcheck "})
	case strings.HasSuffix(name, ".tf"), strings.HasSuffix(name, ".yml"), strings.HasSuffix(name, ".yaml"), name == "Makefile", name == ".dockerignore":
		return prefixedLines(path, "#", nil)
	case name == "Dockerfile":
		return prefixedLines(path, "#", []string{"# syntax="})
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".tmpl"):
		return containing(path, []string{"<!--", "{{/*", "{{- /*"})
	case strings.HasSuffix(name, ".css"):
		return containing(path, []string{"/*"})
	}
	return nil, nil
}

func goComments(path string) ([]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var lines []int
	for _, group := range f.Comments {
		for _, c := range group.List {
			if !goDirective.MatchString(c.Text) {
				lines = append(lines, fset.Position(c.Pos()).Line)
			}
		}
	}
	return lines, nil
}

func migrationVersion(name string) int {
	digits, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}

func prefixedLines(path, prefix string, allowed []string) ([]int, error) {
	return scanLines(path, func(line string) bool {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			return false
		}
		for _, a := range allowed {
			if strings.HasPrefix(trimmed, a) {
				return false
			}
		}
		return true
	})
}

func containing(path string, markers []string) ([]int, error) {
	return scanLines(path, func(line string) bool {
		for _, m := range markers {
			if strings.Contains(line, m) {
				return true
			}
		}
		return false
	})
}

func scanLines(path string, isComment func(string) bool) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []int
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		if isComment(sc.Text()) {
			lines = append(lines, n)
		}
	}
	return lines, sc.Err()
}
