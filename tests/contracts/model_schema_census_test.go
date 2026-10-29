package contracts_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

const conformanceCases = "../conformance/cases"

// censusSchemaStems are the schemas whose every property must appear in a valid conformance case;
// Manifest and Lock keep their own m00 fixtures, and Operation (a schema no model reader loads)
// keeps its own in operation_schema_test.go.
func censusSchemaStems() []string {
	return []string{"definitions", "project", "fragment", "model", "module", "bundle", "bindings", "version-info"}
}

// TestModelSchemaCensus_EveryPropertyAppearsInAValidCase keeps the schemas and the Go readers
// honest: a property no valid case uses is one the readers were never shown, so the strict
// decoding behind every reader has not proved it can read it.
func TestModelSchemaCensus_EveryPropertyAppearsInAValidCase(t *testing.T) {
	t.Parallel()
	used := validCaseKeys(t)
	for _, stem := range censusSchemaStems() {
		for _, property := range schemaProperties(readPublishedSchema(t, stem)) {
			if !used[property] {
				t.Errorf("%s: property %q appears in no valid conformance input or expected output", stem, property)
			}
		}
	}
}

// schemaProperties returns every name declared under a "properties" keyword, at any depth.
func schemaProperties(node any) []string {
	var names []string
	switch value := node.(type) {
	case map[string]any:
		if properties, ok := value["properties"].(map[string]any); ok {
			for name := range properties {
				names = append(names, name)
			}
		}
		for _, child := range value {
			names = append(names, schemaProperties(child)...)
		}
	case []any:
		for _, child := range value {
			names = append(names, schemaProperties(child)...)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// validCaseKeys collects the mapping keys of every input and expected output of the valid and
// parity cases that expect no diagnostics, plus the repository modules those cases load.
func validCaseKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	var roots []string
	for _, group := range []string{"valid", "parity"} {
		entries, err := os.ReadDir(filepath.Join(conformanceCases, group))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			dir := filepath.Join(conformanceCases, group, entry.Name())
			if _, err := os.Stat(filepath.Join(dir, "expected", "diagnostics.json")); err == nil {
				continue
			}
			roots = append(roots, dir)
		}
	}
	roots = append(roots, "../../modules/postgres/module.yaml")
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || entry.Name() == "case.json" {
				return err
			}
			if extension := filepath.Ext(path); extension != ".yaml" && extension != ".json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var document yaml.Node
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Errorf("valid case file %s is not YAML: %v", path, err)
				return nil
			}
			collectMappingKeys(&document, keys)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func collectMappingKeys(node *yaml.Node, keys map[string]bool) {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			keys[node.Content[index].Value] = true
		}
	}
	for _, child := range node.Content {
		collectMappingKeys(child, keys)
	}
}
