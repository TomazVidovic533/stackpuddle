package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/documents"
)

const schemaBase = "urn:stackpuddle:schema:v1:"

type contractDocument map[string]any

type schemaRejectionCase struct {
	name     string
	path     []string
	value    any
	location string
	keyword  string
}

type requiredFieldCase struct {
	name string
	path []string
}

func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()

	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	for _, schemaName := range []string{"definitions", "manifest", "lock", "operation"} {
		path := filepath.Join("..", "..", "schemas", "v1", schemaName+".schema.json")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s schema: %v", schemaName, err)
		}

		var document any
		if err := json.Unmarshal(contents, &document); err != nil {
			t.Fatalf("decode %s schema: %v", schemaName, err)
		}
		if err := compiler.AddResource(schemaBase+schemaName, document); err != nil {
			t.Fatalf("add %s schema: %v", schemaName, err)
		}
	}

	schema, err := compiler.Compile(schemaBase + name)
	if err != nil {
		t.Fatalf("compile %s schema: %v", name, err)
	}
	return schema
}

func loadContractFixture(t *testing.T, path string) contractDocument {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join("testdata", "contracts", "v1", path))
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	value := readFixtureYAML(t, contents)
	document, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected fixture %s to contain an object, got %T", path, value)
	}
	return contractDocument(document)
}

func validateContract(t *testing.T, schemaName string, document contractDocument) error {
	t.Helper()
	return compileSchema(t, schemaName).Validate(map[string]any(document))
}

func assertValidFixture(t *testing.T, schemaName, fixturePath string) {
	t.Helper()

	document := loadContractFixture(t, fixturePath)
	if err := validateContract(t, schemaName, document); err != nil {
		t.Fatalf("expected %s to be valid: %v", fixturePath, err)
	}
}

func assertInvalidFixture(t *testing.T, schemaName, fixturePath, location, keyword string) {
	t.Helper()

	document := loadContractFixture(t, fixturePath)
	err := validateContract(t, schemaName, document)
	if err == nil {
		t.Fatalf("expected %s to fail validation", fixturePath)
	}
	assertValidationFailure(t, err, location, keyword)
}

func assertFixtureMissingRequiredField(t *testing.T, schemaName, fixturePath, location, field string) {
	t.Helper()

	document := loadContractFixture(t, fixturePath)
	err := validateContract(t, schemaName, document)
	if err == nil {
		t.Fatalf("expected %s to fail validation", fixturePath)
	}
	assertRequiredFailure(t, err, location, field)
}

func setContractField(t *testing.T, document contractDocument, value any, path ...string) {
	t.Helper()

	object := documents.ObjectAt(t, map[string]any(document), path[:len(path)-1])
	object[path[len(path)-1]] = value
}

func removeContractField(t *testing.T, document contractDocument, path ...string) {
	t.Helper()

	object := documents.ObjectAt(t, map[string]any(document), path[:len(path)-1])
	delete(object, path[len(path)-1])
}

func assertValidationFailure(t *testing.T, err error, location, keyword string) {
	t.Helper()

	validationError, ok := err.(*jsonschema.ValidationError)
	if !ok {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	if !containsValidationFailure(validationError, location, keyword) {
		t.Fatalf("expected validation failure at %s for %s, got: %v", displayPath(location), keyword, err)
	}
}

func assertRequiredFailure(t *testing.T, err error, location, field string) {
	t.Helper()

	validationError, ok := err.(*jsonschema.ValidationError)
	if !ok {
		t.Fatalf("expected validation error, got %T: %v", err, err)
	}
	if !containsRequiredFailure(validationError, location, field) {
		t.Fatalf("expected required failure for %s at %s, got: %v", field, displayPath(location), err)
	}
}

func containsValidationFailure(err *jsonschema.ValidationError, location, keyword string) bool {
	actualLocation := jsonPointer(err.InstanceLocation)
	if additionalProperties, ok := err.ErrorKind.(*kind.AdditionalProperties); ok && len(additionalProperties.Properties) == 1 {
		actualLocation = joinPath(actualLocation, additionalProperties.Properties[0])
	}
	if actualLocation == location && contains(err.ErrorKind.KeywordPath(), keyword) {
		return true
	}
	for _, cause := range err.Causes {
		if containsValidationFailure(cause, location, keyword) {
			return true
		}
	}
	return false
}

func containsRequiredFailure(err *jsonschema.ValidationError, location, field string) bool {
	if required, ok := err.ErrorKind.(*kind.Required); ok && jsonPointer(err.InstanceLocation) == location && contains(required.Missing, field) {
		return true
	}
	for _, cause := range err.Causes {
		if containsRequiredFailure(cause, location, field) {
			return true
		}
	}
	return false
}

func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}

	escaped := make([]string, len(tokens))
	for index, token := range tokens {
		escaped[index] = strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

func parentLocation(path []string) string {
	return jsonPointer(path[:len(path)-1])
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func joinPath(parent, child string) string {
	escapedChild := strings.ReplaceAll(strings.ReplaceAll(child, "~", "~0"), "/", "~1")
	return parent + "/" + escapedChild
}

func displayPath(path string) string {
	if path == "" {
		return "/"
	}
	return path
}
