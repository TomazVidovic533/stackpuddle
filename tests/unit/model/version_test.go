package model_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/schemas"
)

// definitionSchema compiles one $defs entry of the published definitions schema, so a rule the Go
// code enforces can be checked against the schema rule it mirrors.
func definitionSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	files, err := fs.Glob(schemas.Files(), "v1/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := fs.ReadFile(schemas.Files(), file)
		if err != nil {
			t.Fatal(err)
		}
		document, err := jsonschema.UnmarshalJSON(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		id := "urn:stackpuddle:schema:v1:" + strings.TrimSuffix(strings.TrimPrefix(file, "v1/"), ".schema.json")
		if err := compiler.AddResource(id, document); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile("urn:stackpuddle:schema:v1:definitions#/$defs/" + name)
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	return schema
}

func TestVersionPrecedenceFollowsSemVer(t *testing.T) {
	t.Parallel()
	ascending := []string{
		"0.1.0-alpha.9", "0.1.0-alpha.10", "0.1.0-alpha.beta", "0.1.0",
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11",
		"1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "1.10.0", "2.0.0", "99999999999999999999.0.0",
	}
	for i := 1; i < len(ascending); i++ {
		lower, higher := mustVersion(t, ascending[i-1]), mustVersion(t, ascending[i])
		if lower.Compare(higher) != -1 || higher.Compare(lower) != 1 {
			t.Errorf("%s must sort before %s", ascending[i-1], ascending[i])
		}
	}
}

func TestVersionIgnoresBuildMetadataWhenComparing(t *testing.T) {
	t.Parallel()
	plain, built := mustVersion(t, "1.0.0-alpha.1"), mustVersion(t, "1.0.0-alpha.1+build.7.sha.0a1b")
	if plain.Compare(built) != 0 {
		t.Fatal("build metadata must not affect precedence")
	}
	if built.String() != "1.0.0-alpha.1+build.7.sha.0a1b" {
		t.Fatalf("String() = %q, want the version as written", built.String())
	}
}

// versionTable is shared by the parser test and the schema agreement test.
func versionTable() map[string]bool {
	return map[string]bool{
		"0.1.0-alpha.1": true, "1.0.0": true, "1.0.0-0.3.7": true, "1.0.0-x.7.z.92": true,
		"1.0.0-x-y-z.--": true, "1.0.0+20130313144700": true, "1.0.0-beta+exp.sha.5114f85": true,
		"1.0.0+21AF26D3----117B344092BD": true, "1.0.0+001": true, "18446744073709551616.0.0": true,
		"": false, "1": false, "1.0": false, "1.0.0.0": false, "v1.0.0": false, "01.0.0": false,
		"1.01.0": false, "1.0.01": false, "1.0.0-": false, "1.0.0+": false, "1.0.0-01": false,
		"1.0.0-alpha..1": false, "1.0.0-alpha_1": false, "1.0.0+build..1": false, " 1.0.0": false,
		"1.0.0-beta+": false, "-1.0.0": false, "1.a.0": false,
	}
}

func TestParseVersionAcceptsExactlySemVer(t *testing.T) {
	t.Parallel()
	for text, valid := range versionTable() {
		_, err := model.ParseVersion(text)
		if valid && err != nil {
			t.Errorf("ParseVersion(%q) refused a valid version: %v", text, err)
		}
		if !valid {
			modelError, isModelError := errors.AsType[*model.Error](err)
			if !isModelError || modelError.Code() != model.StructureInvalidValue {
				t.Errorf("ParseVersion(%q) = %v, want a structure.invalid_value *model.Error", text, err)
			}
		}
	}
}

func TestParseVersionAgreesWithTheSchemaSemverFormat(t *testing.T) {
	t.Parallel()
	schema := definitionSchema(t, "semanticVersion")
	for text := range versionTable() {
		_, parseErr := model.ParseVersion(text)
		schemaErr := schema.Validate(text)
		if (parseErr == nil) != (schemaErr == nil) {
			t.Errorf("%q: ParseVersion error %v, schema error %v", text, parseErr, schemaErr)
		}
	}
}

func mustVersion(t *testing.T, text string) model.Version {
	t.Helper()
	version, err := model.ParseVersion(text)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", text, err)
	}
	return version
}
