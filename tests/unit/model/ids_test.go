package model_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

type nameRule struct {
	schemaDef string
	parse     func(string) error
	valid     []string
	invalid   []string
}

func parseErr[T any](parse func(string) (T, error)) func(string) error {
	return func(s string) error {
		_, err := parse(s)
		return err
	}
}

func nameRules() map[string]nameRule {
	scope := nameRule{
		schemaDef: "scopeName",
		valid:     []string{"shop", "a", "local-compose", "s3", strings.Repeat("a", 32)},
		invalid:   []string{"", "shop.dev", "local.compose", "Shop", "3shop", "shop-", "-shop", "shop--dev", "shop_dev", strings.Repeat("a", 33)},
	}
	key := nameRule{
		schemaDef: "objectKey",
		valid:     []string{"api", "web-2", strings.Repeat("k", 30)},
		invalid:   []string{"", "api.v2", "API", "2api", "api_v2", "api-", strings.Repeat("k", 31)},
	}
	withParse := func(rule nameRule, parse func(string) error) nameRule {
		rule.parse = parse
		return rule
	}
	return map[string]nameRule{
		"project":     withParse(scope, parseErr(model.ParseProjectName)),
		"environment": withParse(scope, parseErr(model.ParseEnvironmentName)),
		"object":      withParse(key, parseErr(model.ParseObjectKey)),
		"dependency":  withParse(key, parseErr(model.ParseDependencyKey)),
		"export":      withParse(key, parseErr(model.ParseExportKey)),
		"listener":    withParse(key, parseErr(model.ParseListenerKey)),
		"volume":      withParse(key, parseErr(model.ParseVolumeKey)),
		"input":       withParse(key, parseErr(model.ParseInputKey)),
		"value":       withParse(key, parseErr(model.ParseValueName)),
		"module": {
			schemaDef: "moduleName", parse: parseErr(model.ParseModuleName),
			valid:   []string{"postgres", "queue-cache", strings.Repeat("m", 63)},
			invalid: []string{"", "Postgres", "pg.v2", "pg_main", strings.Repeat("m", 64)},
		},
		"env key": {
			schemaDef: "envKey", parse: parseErr(model.ParseEnvKey),
			valid:   []string{"DATABASE_URL", "_private", "a1", strings.Repeat("A", 128)},
			invalid: []string{"", "1ABC", "API-HOST", "API.HOST", "API HOST", strings.Repeat("A", 129)},
		},
		"capability": {
			schemaDef: "capability", parse: parseErr(model.ParseCapability),
			valid:   []string{"module.postgres/1", "http.api/1", "runtime.compose/12", "a.b.c/3"},
			invalid: []string{"", "postgres/1", "module.postgres", "module.postgres/0", "module.postgres/01", "Module.postgres/1", "module..postgres/1"},
		},
		"data format": {
			schemaDef: "dataFormat", parse: parseErr(model.ParseDataFormat),
			valid:   []string{"postgresql/18", "audit-log/1", "sqlite/3.45.1"},
			invalid: []string{"", "postgresql", "postgresql/", "postgresql/18.", "PostgreSQL/18", "postgresql/v18"},
		},
		"provider": {
			schemaDef: "providerId", parse: parseErr(model.ParseProviderID),
			valid:   []string{"local", "ci-vault", strings.Repeat("p", 32)},
			invalid: []string{"", "local.file", "Local", strings.Repeat("p", 33)},
		},
	}
}

func TestNameRulesMatchTheirSchemaDefinitions(t *testing.T) {
	t.Parallel()
	for name, rule := range nameRules() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schema := definitionSchema(t, rule.schemaDef)
			for _, text := range rule.valid {
				if err := rule.parse(text); err != nil {
					t.Errorf("Parse refused %q: %v", text, err)
				}
				if err := schema.Validate(text); err != nil {
					t.Errorf("schema %s refused %q: %v", rule.schemaDef, text, err)
				}
			}
			for _, text := range rule.invalid {
				err := rule.parse(text)
				if modelError, ok := errors.AsType[*model.Error](err); !ok || modelError.Code() != model.StructureInvalidValue {
					t.Errorf("Parse(%q) = %v, want a structure.invalid_value *model.Error", text, err)
				}
				if schema.Validate(text) == nil {
					t.Errorf("schema %s accepted %q", rule.schemaDef, text)
				}
			}
		})
	}
}

// mixedKeysProgram passes an ExportKey where a DependencyKey belongs.
const mixedKeysProgram = `package main

import "github.com/TomazVidovic533/stackpuddle/model"

func main() {
	export, _ := model.ParseExportKey("database")
	var dependency model.DependencyKey = export
	_ = model.BindingRef{Dependency: dependency}
}
`

// TestIdentifierKindsDoNotMix is the must-not-compile case of the typed identifiers. The program
// is supplied through a build overlay, so no non-test Go file exists under tests/.
func TestIdentifierKindsDoNotMix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte(mixedKeysProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	virtual, err := filepath.Abs(filepath.Join("mustnotcompile", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {virtual: source}})
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("go", "build", "-overlay="+overlayFile, "-o", os.DevNull, "./mustnotcompile").CombinedOutput()
	if err == nil {
		t.Fatal("a program mixing an ExportKey and a DependencyKey compiled")
	}
	if !strings.Contains(string(output), "cannot use") || !strings.Contains(string(output), "model.ExportKey") {
		t.Fatalf("expected a type mismatch naming model.ExportKey, got:\n%s", output)
	}
}
