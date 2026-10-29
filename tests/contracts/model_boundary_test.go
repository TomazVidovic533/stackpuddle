package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// modelBoundaryRoots are the generic core: the packages that evaluate a project. They name no module.
func modelBoundaryRoots() []string {
	return []string{"../../model", "../../schemas", "../../internal/model"}
}

// pureBoundaryRoots are every package that evaluates a project, the core and the module validators:
// they read only what they are given and touch no process, network or file system of their own, so
// a validation failure cannot cause a Docker or database mutation.
func pureBoundaryRoots() []string {
	return append(modelBoundaryRoots(), "../../validation")
}

func forbiddenModelImports() []string {
	return []string{"os", "os/exec", "net", "net/http", "syscall"}
}

func sourceFiles(t *testing.T, roots []string) []*ast.File {
	t.Helper()
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files = append(files, file)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no sources found")
	}
	return files
}

func TestModelBoundary_ImportsNoProcessNetworkOrSystemPackages(t *testing.T) {
	t.Parallel()
	for _, file := range sourceFiles(t, pureBoundaryRoots()) {
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range forbiddenModelImports() {
				if path == forbidden {
					t.Errorf("package %s imports %s", file.Name.Name, path)
				}
			}
		}
	}
}

// TestModelBoundary_NamesNoModule reads every module name from modules/*/module.yaml and fails when
// an identifier or string literal of the core contains one: modules plug into the generic
// contract, and no code path may branch on a particular module.
func TestModelBoundary_NamesNoModule(t *testing.T) {
	t.Parallel()
	names := repositoryModuleNames(t)
	for _, file := range sourceFiles(t, modelBoundaryRoots()) {
		ast.Inspect(file, func(node ast.Node) bool {
			var text string
			switch value := node.(type) {
			case *ast.Ident:
				text = value.Name
			case *ast.BasicLit:
				if value.Kind != token.STRING {
					return true
				}
				text = value.Value
			default:
				return true
			}
			for _, name := range names {
				if strings.Contains(strings.ToLower(text), name) {
					t.Errorf("package %s names module %q in %s", file.Name.Name, name, text)
				}
			}
			return true
		})
	}
}

func repositoryModuleNames(t *testing.T) []string {
	t.Helper()
	descriptors, err := filepath.Glob("../../modules/*/module.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, descriptor := range descriptors {
		data, err := os.ReadFile(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		var module struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(data, &module); err != nil || module.Name == "" {
			t.Fatalf("read the name of %s: %v", descriptor, err)
		}
		names = append(names, module.Name)
	}
	if len(names) == 0 {
		t.Fatal("no modules found under modules/")
	}
	return names
}
