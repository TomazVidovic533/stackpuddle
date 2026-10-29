package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/validation/postgres"
)

// The structural tests accept these inputs on purpose (TestPostgresConfigurationSchemaLeavesReferencesToSemanticValidation):
// the JSON Schema proves shape, not relationships. Here the same ten inputs must each fail the
// semantic validator with their stable code, so no relationship goes unchecked.
func TestPostgresSemanticValidationRejectsWhatTheSchemaLeavesOpen(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	tests := []struct {
		name  string
		path  []string
		value any
		code  string
	}{
		{"missing owner", []string{"databases", "application", "owner"}, "missing_migrator", "postgres.unknown_owner"},
		{"runtime owner", []string{"databases", "application", "owner"}, "app_runtime", "postgres.owner_not_migration_user"},
		{"unknown membership", []string{"users", "app_runtime", "memberOf"}, []any{"missing_role"}, "postgres.unknown_member_role"},
		{"unknown connection role", []string{"databases", "application", "connectRoles"}, []any{"missing_role"}, "postgres.unknown_connect_role"},
		{"grant without connect", []string{"databases", "application", "connectRoles"}, []any{"reporting_reader"}, "postgres.grantee_without_connect"},
		{"unknown grant role", []string{"databases", "application", "schemas", "app", "grants"}, map[string]any{"missing_role": map[string]any{"tables": []any{"SELECT"}}}, "postgres.unknown_grantee"},
		{"unknown settings user", []string{"databases", "application", "userSettings"}, map[string]any{"missing_user": map[string]any{"statementTimeoutMs": 1}}, "postgres.unknown_settings_user"},
		{"owner reused across databases", []string{"databases", "reporting", "owner"}, "app_migrator", "postgres.migration_user_owns_multiple_databases"},
		{"settings user without connection", []string{"databases", "application", "userSettings"}, map[string]any{"reporting_app": map[string]any{"lockTimeoutMs": 100}}, "postgres.settings_user_without_access"},
		{"user role name collision", []string{"roles", "app_runtime"}, map[string]any{"login": false}, "postgres.shared_role_name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := postgresFixture(t, "multiple-databases.yaml")
			setContractField(t, document, test.value, test.path...)
			if err := schema.Validate(map[string]any(document)); err != nil {
				t.Fatalf("the schema must accept this input, as the structural test requires: %v", err)
			}
			diagnostics, err := postgres.Validate(encodeContractDocument(t, document))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			var codes []string
			for _, diagnostic := range diagnostics {
				codes = append(codes, string(diagnostic.Code()))
			}
			if !slices.Contains(codes, test.code) {
				t.Errorf("want %s among %v", test.code, codes)
			}
		})
	}
}

// TestPostgresValidFixturesDecodeAndValidateClean covers the wire struct of the semantic rules
// against the schema: each valid fixture decodes with no unknown field and has no semantic finding,
// and every-field.yaml sets every property the schema declares, so a property added to the schema
// without the wire struct fails here.
func TestPostgresValidFixturesDecodeAndValidateClean(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	fixtures, err := filepath.Glob(filepath.Join("testdata", "modules", "postgres", "valid", "*.yaml"))
	if err != nil || len(fixtures) < 4 {
		t.Fatalf("valid fixtures = %v, %v; want the three examples and every-field.yaml", fixtures, err)
	}
	for _, fixture := range fixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			t.Parallel()
			document := postgresFixture(t, filepath.Base(fixture))
			if err := schema.Validate(map[string]any(document)); err != nil {
				t.Fatalf("schema: %v", err)
			}
			diagnostics, err := postgres.Validate(encodeContractDocument(t, document))
			if err != nil {
				t.Fatalf("the wire struct does not decode a schema-valid fixture: %v", err)
			}
			if len(diagnostics) != 0 {
				t.Errorf("valid fixture reported %v", diagnostics)
			}
		})
	}
}

func TestPostgresEveryFieldFixtureSetsEverySchemaProperty(t *testing.T) {
	t.Parallel()
	schemaDocument := readSchemaDocument(t)
	declared := map[string]bool{}
	collectSchemaPaths(schemaDocument, schemaDocument, "", declared)
	if len(declared) < 20 {
		t.Fatalf("found only %d schema paths; the walk is wrong", len(declared))
	}
	covered := map[string]bool{}
	fixture := map[string]any(postgresFixture(t, "every-field.yaml"))
	collectFixturePaths(schemaDocument, schemaDocument, fixture, "", covered)
	for path := range declared {
		if !covered[path] {
			t.Errorf("every-field.yaml does not set schema property %s", path)
		}
	}
}

func encodeContractDocument(t *testing.T, document contractDocument) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any(document))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readSchemaDocument(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "modules", "postgres", "content", "configuration", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// resolveSchema follows a local "#/$defs/<name>" reference, once, and returns the node itself
// otherwise.
func resolveSchema(root, node map[string]any) map[string]any {
	reference, _ := node["$ref"].(string)
	const prefix = "#/$defs/"
	if len(reference) <= len(prefix) || reference[:len(prefix)] != prefix {
		return node
	}
	defs, _ := root["$defs"].(map[string]any)
	target, _ := defs[reference[len(prefix):]].(map[string]any)
	return target
}

// collectSchemaPaths records the path of every named property and every map-valued member
// ("*" for a key chosen by the user) of the schema.
func collectSchemaPaths(root, node map[string]any, prefix string, out map[string]bool) {
	node = resolveSchema(root, node)
	if properties, ok := node["properties"].(map[string]any); ok {
		for name, child := range properties {
			path := prefix + "/" + name
			out[path] = true
			if object, ok := child.(map[string]any); ok {
				collectSchemaPaths(root, object, path, out)
			}
		}
	}
	if values, ok := node["additionalProperties"].(map[string]any); ok {
		path := prefix + "/*"
		out[path] = true
		collectSchemaPaths(root, values, path, out)
	}
}

// collectFixturePaths records the schema paths the fixture sets, walking schema and fixture together.
func collectFixturePaths(root, node map[string]any, value any, prefix string, out map[string]bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	node = resolveSchema(root, node)
	properties, _ := node["properties"].(map[string]any)
	values, _ := node["additionalProperties"].(map[string]any)
	for key, member := range object {
		if child, named := properties[key].(map[string]any); named {
			path := prefix + "/" + key
			out[path] = true
			collectFixturePaths(root, child, member, path, out)
			continue
		}
		if values != nil {
			path := prefix + "/*"
			out[path] = true
			collectFixturePaths(root, values, member, path, out)
		}
	}
}
