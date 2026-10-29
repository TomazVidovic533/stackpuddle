package conformance_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// cliOnlyCode is reported by model.ParseRef for command-line arguments, never by a document.
const cliOnlyCode = "reference.invalid_ref"

// minimumLeakLength skips short scalars such as "1" or "yes" that occur in fixed message text.
const minimumLeakLength = 4

// TestEveryCodeHasAConformanceCase reads the Code constants from the model sources, so a new code
// without a case fails here without anyone updating a list.
func TestEveryCodeHasAConformanceCase(t *testing.T) {
	t.Parallel()
	declared := declaredCodes(t)
	if len(declared) == 0 {
		t.Fatal("no Code constants found in the model package")
	}
	covered := map[string]bool{}
	for _, dir := range listCases(t) {
		for _, name := range []string{expectedDiags, expectedResolve} {
			data, ok := readExpected(t, dir, name)
			if !ok {
				continue
			}
			var diagnostics []expectedDiagnostic
			if err := json.Unmarshal(data, &diagnostics); err != nil {
				t.Fatalf("%s: decode expected/%s: %v", dir.name, name, err)
			}
			for _, diagnostic := range diagnostics {
				covered[diagnostic.Code] = true
			}
		}
	}
	for _, code := range declared {
		if code != cliOnlyCode && !covered[code] {
			t.Errorf("code %s has no conformance case", code)
		}
	}
	for code := range covered {
		if !slices.Contains(declared, code) {
			t.Errorf("a case expects %s, which the model does not declare", code)
		}
	}
}

func declaredCodes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repositoryRoot, "model", "*.go"))
	if err != nil {
		t.Fatalf("list model sources: %v", err)
	}
	moduleFiles, err := filepath.Glob(filepath.Join(repositoryRoot, "validation", "*", "*.go"))
	if err != nil {
		t.Fatalf("list module validator sources: %v", err)
	}
	files = append(files, moduleFiles...)
	var codes []string
	fileSet := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				codes = append(codes, codeValues(t, spec)...)
			}
		}
	}
	slices.Sort(codes)
	return codes
}

// isCodeType reports whether expression names the model's Code type, as "Code" inside the model and
// "model.Code" in a module validator.
func isCodeType(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name == "Code"
	case *ast.SelectorExpr:
		qualifier, ok := typed.X.(*ast.Ident)
		return ok && qualifier.Name == "model" && typed.Sel.Name == "Code"
	default:
		return false
	}
}

func codeValues(t *testing.T, spec ast.Spec) []string {
	t.Helper()
	value, ok := spec.(*ast.ValueSpec)
	if !ok {
		return nil
	}
	if !isCodeType(value.Type) {
		return nil
	}
	var codes []string
	for _, expression := range value.Values {
		literal, ok := expression.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatalf("Code constant %s is not a string literal", value.Names[0].Name)
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", literal.Value, err)
		}
		codes = append(codes, text)
	}
	return codes
}

// TestDiagnosticsNeverEchoDeclaredValues loads every case that expects diagnostics and checks that
// no message contains a value written under environment, inputs values, options, parameters or
// command in the case's inputs. Values that are also mapping keys are identifiers and may appear.
func TestDiagnosticsNeverEchoDeclaredValues(t *testing.T) {
	t.Parallel()
	for _, dir := range listCases(t) {
		_, expects := readExpected(t, dir, expectedDiags)
		if !expects || (dir.spec.Subject != "project" && dir.spec.Subject != "semantics") {
			continue
		}
		t.Run(dir.name, func(t *testing.T) {
			t.Parallel()
			values := sensitiveValues(t, dir)
			for _, diagnostic := range caseDiagnostics(t, dir) {
				for _, value := range values {
					if strings.Contains(diagnostic.Message(), value) {
						t.Errorf("%s message repeats a declared value: %q", diagnostic.Code(), diagnostic.Message())
					}
				}
			}
		})
	}
}

func TestRedactionCanaryIsCovered(t *testing.T) {
	t.Parallel()
	canaries := map[string][]string{
		"invalid":   {"canary-7f3a9c"},
		"semantics": {"canary-sp-secret", "canary_ghost_role", "canary_ghost_owner", "7319"},
	}
	for group, want := range canaries {
		t.Run(group, func(t *testing.T) {
			t.Parallel()
			dir := caseDir{name: group + "/redaction-canary", path: filepath.Join(casesDir, group, "redaction-canary")}
			dir.spec = readCaseSpec(t, dir.path)
			values := sensitiveValues(t, dir)
			for _, canary := range want {
				if !slices.Contains(values, canary) {
					t.Errorf("the %s canary case no longer declares %q; values: %v", group, canary, values)
				}
			}
		})
	}
}

// sensitiveValues collects the string scalars of the case's YAML and JSON inputs that sit under a
// value-bearing member.
func sensitiveValues(t *testing.T, dir caseDir) []string {
	t.Helper()
	keys := map[string]bool{}
	var candidates []string
	err := filepath.WalkDir(dir.path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == expectedDir {
			return filepath.SkipDir
		}
		extension := filepath.Ext(path)
		if entry.IsDir() || entry.Name() == "case.json" || (extension != ".yaml" && extension != ".yml" && extension != ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var document yaml.Node
		if yaml.Unmarshal(data, &document) != nil {
			// Files that are not YAML only reach the syntax stage, whose messages are fixed text.
			return nil
		}
		collectScalars(&document, nil, keys, &candidates)
		return nil
	})
	if err != nil {
		t.Fatalf("walk case inputs: %v", err)
	}
	var values []string
	for _, candidate := range candidates {
		if len(candidate) >= minimumLeakLength && !keys[candidate] && !slices.Contains(values, candidate) {
			values = append(values, candidate)
		}
	}
	return values
}

func collectScalars(node *yaml.Node, path []string, keys map[string]bool, values *[]string) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			collectScalars(child, path, keys, values)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			keys[key] = true
			collectScalars(node.Content[index+1], append(slices.Clone(path), key), keys, values)
		}
	case yaml.ScalarNode:
		if valueBearing(path) {
			*values = append(*values, node.Value)
		}
	}
}

// valueBearing reports whether the scalar at path is a declared value rather than an identifier:
// anything under options, parameters or command, an input's value, or an environment variable's
// literal. An {input: <key>} reference and a delivery's value names are identifiers.
func valueBearing(path []string) bool {
	for index, segment := range path {
		switch segment {
		case "options", "parameters", "command":
			return true
		case "environment":
			delivered := index > 0 && path[index-1] == "delivery"
			reference := path[len(path)-1] == "input"
			return !delivered && !reference
		case "inputs":
			return path[len(path)-1] == "value"
		}
	}
	return false
}
