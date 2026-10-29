package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const layeredProject = `apiVersion: stackpuddle/v1
kind: Project
metadata:
  name: shop
spec:
  modules:
    layered:
      definition: layered
      options:
        replaced: 20
        list: [9]
        nested:
          b: 3
          c: 4
  environments:
    local:
      runtime: {type: compose, context: desktop-linux}
      secrets: {provider: local}
      overrides:
        modules:
          layered:
            options:
              replaced: 30
              nested:
                c: 5
    plain:
      runtime: {type: compose, context: desktop-linux}
      secrets: {provider: local}
`

const layeredModule = `apiVersion: stackpuddle/v1
kind: Module
name: layered
version: 0.1.0
configuration:
  schema: content/options.schema.json
  defaults: content/defaults.yaml
`

const layeredDefaults = `scalar: 1
replaced: 10
list: [1, 2, 3]
nested:
  a: 1
  b: 2
`

func layeredFixture(t *testing.T) (model.Project, model.ObjectKey) {
	t.Helper()
	definition, diagnostics := model.LoadModuleDefinition(fstest.MapFS{
		"module.yaml":                 {Data: []byte(layeredModule)},
		"content/options.schema.json": {Data: []byte(`{"type": "object"}`)},
		"content/defaults.yaml":       {Data: []byte(layeredDefaults)},
	})
	if len(diagnostics) > 0 {
		t.Fatalf("definition: %v", diagnostics)
	}
	project, diagnostics := model.Load(fstest.MapFS{"stackpuddle.yaml": {Data: []byte(layeredProject)}}, "stackpuddle.yaml",
		[]model.ModuleDefinition{definition})
	if len(diagnostics) > 0 {
		t.Fatalf("project: %v", diagnostics)
	}
	return project, mustObjectKey(t, "layered")
}

func mustObjectKey(t *testing.T, text string) model.ObjectKey {
	t.Helper()
	key, err := model.ParseObjectKey(text)
	if err != nil {
		t.Fatalf("ParseObjectKey(%q): %v", text, err)
	}
	return key
}

func mustEnvironment(t *testing.T, text string) model.EnvironmentName {
	t.Helper()
	name, err := model.ParseEnvironmentName(text)
	if err != nil {
		t.Fatalf("ParseEnvironmentName(%q): %v", text, err)
	}
	return name
}

func decodeOptions(t *testing.T, options model.EffectiveOptions) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(options.JSON(), &value); err != nil {
		t.Fatalf("decode %s: %v", options.JSON(), err)
	}
	return value
}

func TestEffectiveOptionsLayersDefaultsUnderDeclaredOptions(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	options, ok := project.EffectiveOptions(key)
	if !ok {
		t.Fatal("EffectiveOptions: ok = false")
	}
	want := map[string]any{
		"scalar":   1.0,
		"replaced": 20.0,
		"list":     []any{9.0},
		"nested":   map[string]any{"a": 1.0, "b": 3.0, "c": 4.0},
	}
	if got := decodeOptions(t, options); !reflect.DeepEqual(want, got) {
		t.Errorf("project-level options: want %v, got %v", want, got)
	}
	if _, has := options.Environment(); has {
		t.Error("project-level options name an environment")
	}
	if options.Instance() != key || options.Definition().String() != "layered" {
		t.Errorf("Instance/Definition = %s/%s", options.Instance(), options.Definition())
	}
}

func TestEnvironmentOptionsAppliesTheOverrideLast(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	local := mustEnvironment(t, "local")
	options, ok := project.EnvironmentOptions(local, key)
	if !ok {
		t.Fatal("EnvironmentOptions: ok = false")
	}
	want := map[string]any{
		"scalar":   1.0,
		"replaced": 30.0,
		"list":     []any{9.0},
		"nested":   map[string]any{"a": 1.0, "b": 3.0, "c": 5.0},
	}
	if got := decodeOptions(t, options); !reflect.DeepEqual(want, got) {
		t.Errorf("environment options: want %v, got %v", want, got)
	}
	if name, has := options.Environment(); !has || name != local {
		t.Errorf("Environment() = %v, %v", name, has)
	}
}

func TestEnvironmentOptionsWithoutOverrideEqualsProjectLevel(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	plain := mustEnvironment(t, "plain")
	projectLevel, _ := project.EffectiveOptions(key)
	options, ok := project.EnvironmentOptions(plain, key)
	if !ok {
		t.Fatal("EnvironmentOptions: ok = false")
	}
	if string(options.JSON()) != string(projectLevel.JSON()) {
		t.Errorf("bytes differ:\nproject %s\nplain   %s", projectLevel.JSON(), options.JSON())
	}
	if name, has := options.Environment(); !has || name != plain {
		t.Errorf("Environment() = %v, %v", name, has)
	}
}

func TestOptionSourceNamesLayerAndLocation(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	local := mustEnvironment(t, "local")
	options, _ := project.EnvironmentOptions(local, key)
	const overrides = "/spec/environments/local/overrides/modules/layered/options"
	const declared = "/spec/modules/layered/options"
	tests := []struct {
		name        string
		pointer     string
		layer       model.OptionLayer
		file        string
		sourcePoint string
	}{
		{"default scalar", "/scalar", model.OptionLayerDefault, "content/defaults.yaml", "/scalar"},
		{"default object member kept", "/nested/a", model.OptionLayerDefault, "content/defaults.yaml", "/nested/a"},
		{"environment replaces a declared scalar", "/replaced", model.OptionLayerEnvironment, "stackpuddle.yaml", overrides + "/replaced"},
		{"declared member of a merged object", "/nested/b", model.OptionLayerProject, "stackpuddle.yaml", declared + "/nested/b"},
		{"environment member of a merged object", "/nested/c", model.OptionLayerEnvironment, "stackpuddle.yaml", overrides + "/nested/c"},
		{"declared list replaces the default list", "/list/0", model.OptionLayerProject, "stackpuddle.yaml", declared + "/list/0"},
		{"absent member resolves to its nearest ancestor", "/list/5", model.OptionLayerProject, "stackpuddle.yaml", declared + "/list"},
		{"absent member of a merged object", "/nested/zzz", model.OptionLayerEnvironment, "stackpuddle.yaml", overrides + "/nested"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := options.Source(test.pointer)
			if source.Layer != test.layer {
				t.Errorf("Source(%s).Layer = %s, want %s", test.pointer, source.Layer, test.layer)
			}
			location := source.Location
			if location.File() != test.file || location.Pointer() != test.sourcePoint {
				t.Errorf("Source(%s).Location = %s %s, want %s %s", test.pointer, location.File(), location.Pointer(), test.file, test.sourcePoint)
			}
			if _, _, hasLine := location.Position(); !hasLine {
				t.Errorf("Source(%s).Location has no line", test.pointer)
			}
		})
	}
}

func TestOptionLayerStringsAreStable(t *testing.T) {
	t.Parallel()
	got := []string{model.OptionLayerDefault.String(), model.OptionLayerProject.String(), model.OptionLayerEnvironment.String()}
	if want := []string{"default", "project", "environment"}; !reflect.DeepEqual(want, got) {
		t.Errorf("layer names: want %v, got %v", want, got)
	}
}

func TestEffectiveOptionsReportsNotOKForWhatIsNotThere(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	unresolved := unresolvedFixture(t)
	tests := []struct {
		name string
		call func() bool
	}{
		{"unknown instance", func() bool { _, ok := project.EffectiveOptions(mustObjectKey(t, "missing")); return ok }},
		{"unknown instance in an environment", func() bool {
			_, ok := project.EnvironmentOptions(mustEnvironment(t, "local"), mustObjectKey(t, "missing"))
			return ok
		}},
		{"unknown environment", func() bool { _, ok := project.EnvironmentOptions(mustEnvironment(t, "nowhere"), key); return ok }},
		{"unresolved definition", func() bool { _, ok := unresolved.EffectiveOptions(mustObjectKey(t, "db")); return ok }},
		{"unresolved definition in an environment", func() bool {
			_, ok := unresolved.EnvironmentOptions(mustEnvironment(t, "local"), mustObjectKey(t, "db"))
			return ok
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.call() {
				t.Error("ok = true, want false")
			}
		})
	}
}

func unresolvedFixture(t *testing.T) model.Project {
	t.Helper()
	root := filepath.Join("..", "..", "conformance", "cases", "valid", "unresolved-packaged")
	project, diagnostics := model.Load(os.DirFS(root), "stackpuddle.yaml", nil)
	if len(diagnostics) > 0 {
		t.Fatalf("unresolved-packaged: %v", diagnostics)
	}
	if len(project.Unresolved()) == 0 {
		t.Fatal("fixture has no unresolved definition")
	}
	return project
}

func TestEffectiveOptionsJSONReturnsACopy(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	options, _ := project.EffectiveOptions(key)
	first := options.JSON()
	want := string(first)
	first[0] = 'X'
	if got := string(options.JSON()); got != want {
		t.Errorf("JSON changed after mutating a returned copy: %q, want %q", got, want)
	}
}

func TestEnvironmentOptionsOfTheInputsAndOverridesCase(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "conformance", "cases", "valid", "inputs-and-overrides")
	packaged := filepath.Join("..", "..", "..", "modules", "postgres")
	postgres, diagnostics := model.LoadModuleDefinition(os.DirFS(packaged))
	if len(diagnostics) > 0 {
		t.Fatalf("postgres definition: %v", diagnostics)
	}
	project, diagnostics := model.Load(os.DirFS(root), "stackpuddle.yaml", []model.ModuleDefinition{postgres})
	if len(diagnostics) > 0 {
		t.Fatalf("project: %v", diagnostics)
	}
	options, ok := project.EnvironmentOptions(mustEnvironment(t, "local"), mustObjectKey(t, "primary"))
	if !ok {
		t.Fatal("EnvironmentOptions: ok = false")
	}
	value := decodeOptions(t, options)
	if got := value["startupTimeoutSeconds"]; got != 120.0 {
		t.Errorf("startupTimeoutSeconds = %v, want 120 from the environment", got)
	}
	settings, _ := value["settings"].(map[string]any)
	if got := settings["maxConnections"]; got != 100.0 {
		t.Errorf("settings.maxConnections = %v, want 100 from the default", got)
	}
	if layer := options.Source("/settings/maxConnections").Layer; layer != model.OptionLayerDefault {
		t.Errorf("settings.maxConnections layer = %s, want default", layer)
	}
	if layer := options.Source("/startupTimeoutSeconds").Layer; layer != model.OptionLayerEnvironment {
		t.Errorf("startupTimeoutSeconds layer = %s, want environment", layer)
	}
	if diagnostics := postgres.ValidateOptions(options.JSON()); len(diagnostics) > 0 {
		t.Errorf("the effective options do not satisfy the schema: %v", diagnostics)
	}
}

func TestDiagnosticAtMovesTheLocationAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	project, key := layeredFixture(t)
	options, _ := project.EffectiveOptions(key)
	definition, _ := project.Definition(options.Definition())
	diagnostics := definition.ValidateOptions(json.RawMessage(`[]`))
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want one", diagnostics)
	}
	original := diagnostics[0]
	moved := original.At(options.Source("/replaced").Location)
	if moved.Code() != original.Code() || moved.Message() != original.Message() {
		t.Errorf("At changed code or message: %v", moved)
	}
	if moved.Location().File() != "stackpuddle.yaml" || original.Location().File() != "" {
		t.Errorf("At location = %q, original = %q", moved.Location().File(), original.Location().File())
	}
}
