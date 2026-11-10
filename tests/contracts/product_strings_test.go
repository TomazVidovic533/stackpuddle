package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var trackerID = regexp.MustCompile(`SP-M\d{2}-\d{3}`)

// TestNoTrackerIDsInProductStrings fails on a planning tracker ID (SP-Mnn-nnn) inside any string
// literal of the module's non-test Go sources: those strings reach users, who cannot resolve the ID.
// Comments may cite one.
func TestNoTrackerIDsInProductStrings(t *testing.T) {
	t.Parallel()
	root := "../.."
	fileSet := token.NewFileSet()
	parsed := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		parsed++
		ast.Inspect(file, func(node ast.Node) bool {
			if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if id := trackerID.FindString(lit.Value); id != "" {
					t.Errorf("%s: string literal names tracker ID %s; describe the behaviour instead", fileSet.Position(lit.Pos()), id)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if parsed == 0 {
		t.Fatal("no sources found")
	}
}
