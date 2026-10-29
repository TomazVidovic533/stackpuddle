package model_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
	"github.com/TomazVidovic533/stackpuddle/internal/model/yamlvalue"
	"github.com/TomazVidovic533/stackpuddle/schemas"
)

const (
	testSchemaID = "urn:test:options"
	checkLimit   = 100
)

func compileTestSchema(t *testing.T, schema string) *schemacheck.Set {
	t.Helper()
	tree := decodeTree(t, schema)
	set, err := schemacheck.CompileDocument(testSchemaID, tree)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return set
}

func decodeTree(t *testing.T, document string) any {
	t.Helper()
	tree, _, violations := yamlvalue.Decode([]byte(document), defaultLimits())
	if len(violations) > 0 {
		t.Fatalf("decode %q: %+v", document, violations)
	}
	return tree
}

func check(t *testing.T, set *schemacheck.Set, id, document string) []schemacheck.Violation {
	t.Helper()
	result, err := set.Check(id, decodeTree(t, document), checkLimit)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.Truncated {
		t.Fatalf("check stopped at its limit of %d", checkLimit)
	}
	violations := result.Violations
	for index := range violations {
		violations[index].Expected = ""
	}
	return violations
}

func TestCheckReportsOneViolationPerLeaf(t *testing.T) {
	t.Parallel()
	set := compileTestSchema(t, `{
		"type": "object", "additionalProperties": false, "required": ["name", "port"],
		"properties": {"name": {"type": "string", "pattern": "^[a-z]+$"}, "port": {"type": "integer", "maximum": 65535},
		               "tags": {"type": "array", "items": {"type": "string"}}}
	}`)
	got := check(t, set, testSchemaID, `{name: Shop, tags: [ok, 3], extra: true}`)
	want := []schemacheck.Violation{
		{Pointer: "/extra", Field: "extra", Kind: schemacheck.UnknownField},
		{Pointer: "/name", Kind: schemacheck.InvalidValue},
		{Pointer: "/port", Field: "port", Kind: schemacheck.MissingField},
		{Pointer: "/tags/1", Kind: schemacheck.InvalidType},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestCheckCollapsesOneOfToOneViolation(t *testing.T) {
	t.Parallel()
	set := compileTestSchema(t, `{
		"type": "object",
		"properties": {"image": {"type": "string"}, "build": {"type": "object"}},
		"oneOf": [{"required": ["image"], "not": {"required": ["build"]}}, {"required": ["build"], "not": {"required": ["image"]}}]
	}`)
	for _, document := range []string{`{image: a, build: {}}`, `{}`} {
		got := check(t, set, testSchemaID, document)
		want := []schemacheck.Violation{{Pointer: "", Kind: schemacheck.ExactlyOne}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %+v, want one exactly-one violation", document, got)
		}
	}
}

// TestCheckDoesNotReportKnownFieldsAsUnknown covers a referenced schema that fails: it then marks
// none of its properties as evaluated, so unevaluatedProperties would call each of them unknown.
func TestCheckDoesNotReportKnownFieldsAsUnknown(t *testing.T) {
	t.Parallel()
	set := compileTestSchema(t, `{
		"$defs": {"spec": {"type": "object", "required": ["type"], "properties": {"type": {"type": "string"}, "image": {"type": "string"}}}},
		"$ref": "#/$defs/spec", "properties": {"metadata": {"type": "object"}}, "unevaluatedProperties": false
	}`)
	got := check(t, set, testSchemaID, `{image: a, metadata: {}}`)
	want := []schemacheck.Violation{{Pointer: "/type", Field: "type", Kind: schemacheck.MissingField}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want only the missing type", got)
	}
	got = check(t, set, testSchemaID, `{type: service, colour: red}`)
	want = []schemacheck.Violation{{Pointer: "/colour", Field: "colour", Kind: schemacheck.UnknownField}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want the one unknown field", got)
	}
}

// TestCheckKeepsTheSameViolationsAtItsLimit covers a report cut at the limit: the validator
// visits object members in map order, so without a fixed walk order each run would keep others.
func TestCheckKeepsTheSameViolationsAtItsLimit(t *testing.T) {
	t.Parallel()
	const fields, limit, runs = 300, 10, 20
	set := compileTestSchema(t, `{"type": "object", "additionalProperties": {"type": "string"}, "propertyNames": {"maxLength": 3}}`)
	var document strings.Builder
	document.WriteString("{")
	for index := range fields {
		fmt.Fprintf(&document, "f%03d: %d, ", index, index)
	}
	document.WriteString("long-name: x}")
	tree := decodeTree(t, document.String())
	var first schemacheck.Result
	for run := range runs {
		result, err := set.Check(testSchemaID, tree, limit)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Truncated || len(result.Violations) != limit {
			t.Fatalf("want %d violations and a truncated result, got %d and %t", limit, len(result.Violations), result.Truncated)
		}
		if run == 0 {
			first = result
			continue
		}
		if !reflect.DeepEqual(result, first) {
			t.Fatalf("run %d kept other violations:\n%+v\nwant\n%+v", run, result.Violations, first.Violations)
		}
	}
}

// TestCheckLocatesPropertyNameFailures guards the workaround for the validator's shared location
// slice: later siblings overwrite a propertyNames failure's recorded location.
func TestCheckLocatesPropertyNameFailures(t *testing.T) {
	t.Parallel()
	set, err := schemacheck.CompileFS(schemas.Files(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	document := `{apiVersion: stackpuddle/v1, kind: Project, metadata: {name: shop},
		spec: {environments: {local.compose: {runtime: {type: compose, context: default}, secrets: {provider: local}}},
		       workloads: {api: {type: service, image: a, listeners: {Bad_Key: {port: 80, protocol: http}}}}}}`
	got := check(t, set, "urn:stackpuddle:schema:v1:project", document)
	want := []schemacheck.Violation{
		{Pointer: "/spec/environments/local.compose", Kind: schemacheck.InvalidValue},
		{Pointer: "/spec/workloads/api/listeners/Bad_Key", Kind: schemacheck.InvalidValue},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// TestCompileRefusesFileReferences is the S5 failure path: an option schema that refers to a local
// file must not compile, even when the file exists and holds a valid schema, so nothing is read.
func TestCompileRefusesFileReferences(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "readable.schema.json")
	if err := os.WriteFile(target, []byte(`{"type": "string"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"file://" + filepath.ToSlash(target), "https://example.test/schema.json"} {
		_, err := schemacheck.CompileDocument(testSchemaID, decodeTree(t, `{"$ref": "`+reference+`"}`))
		if !errors.Is(err, schemacheck.ErrLoadRefused) {
			t.Errorf("$ref %s: got %v, want the load refused", reference, err)
		}
	}
	if _, err := schemacheck.CompileDocument(testSchemaID, decodeTree(t, `{"$schema": "https://example.test/draft"}`)); err == nil {
		t.Error("an unknown meta-schema must not compile")
	}
}
