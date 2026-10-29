package validation_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/validation"
)

const (
	postgresDir   = "../../../modules/postgres"
	conformanceAt = "../../conformance/cases"
)

// projectText is a project with one postgres instance "db". owner is the application database's
// owner; environments is inserted under spec.environments.
func projectText(owner, environments string) string {
	return `apiVersion: stackpuddle/v1
kind: Project
metadata:
  name: shop
spec:
  modules:
    db:
      definition: postgres
      options:
        users:
          app: {purpose: runtime, passwordSecretRef: app-password, memberOf: [writer]}
          migrator: {purpose: migrations, passwordSecretRef: migrator-password}
        roles:
          writer: {login: false}
        databases:
          application:
            owner: ` + owner + `
            connectRoles: [writer]
            schemas:
              public:
                grants:
                  writer: {tables: [SELECT]}
  environments:
` + environments
}

const (
	plainEnvironment = `    local:
      runtime: {type: compose, context: desktop-linux}
      secrets: {provider: local}
`
	ghostOverride = `    local:
      runtime: {type: compose, context: desktop-linux}
      secrets: {provider: local}
      overrides:
        modules:
          db:
            options:
              databases:
                application:
                  owner: ghost
`
	timeoutOverride = `    local:
      runtime: {type: compose, context: desktop-linux}
      secrets: {provider: local}
      overrides:
        modules:
          db:
            options:
              startupTimeoutSeconds: 120
`
)

// postgresFiles copies the repository's postgres module, replacing the named files.
func postgresFiles(t *testing.T, replace map[string]string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	root := os.DirFS(postgresDir)
	err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, path)
		files[path] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		t.Fatalf("copy postgres module: %v", err)
	}
	for path, text := range replace {
		files[path] = &fstest.MapFile{Data: []byte(text)}
	}
	return files
}

func loadDefinition(t *testing.T, fsys fs.FS) model.ModuleDefinition {
	t.Helper()
	definition, diagnostics := model.LoadModuleDefinition(fsys)
	if len(diagnostics) > 0 {
		t.Fatalf("definition: %v", diagnostics)
	}
	return definition
}

func loadProject(t *testing.T, fsys fs.FS, root string, packaged ...model.ModuleDefinition) model.Project {
	t.Helper()
	project, diagnostics := model.Load(fsys, root, packaged)
	if len(diagnostics) > 0 {
		t.Fatalf("project: %v", diagnostics)
	}
	return project
}

func postgresProject(t *testing.T, text string, replace map[string]string) model.Project {
	t.Helper()
	return loadProject(t, fstest.MapFS{"stackpuddle.yaml": {Data: []byte(text)}}, "stackpuddle.yaml", loadDefinition(t, postgresFiles(t, replace)))
}

func validate(t *testing.T, project model.Project) validation.Result {
	t.Helper()
	result, err := validation.Validate(project)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return result
}

type found struct{ Code, File, Pointer string }

func summary(diagnostics model.Diagnostics) []found {
	var list []found
	for _, diagnostic := range diagnostics {
		list = append(list, found{string(diagnostic.Code()), diagnostic.Location().File(), diagnostic.Location().Pointer()})
	}
	return list
}

func names(modules []model.ModuleName) []string {
	var list []string
	for _, module := range modules {
		list = append(list, module.String())
	}
	return list
}

func TestModulesListsTheCatalogSorted(t *testing.T) {
	t.Parallel()
	got := names(validation.Modules())
	if want := []string{"postgres"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Modules() = %v, want %v", got, want)
	}
}

func TestValidateAcceptsAValidProject(t *testing.T) {
	t.Parallel()
	result := validate(t, postgresProject(t, projectText("migrator", plainEnvironment), nil))
	if len(result.Diagnostics) != 0 || len(result.Unchecked) != 0 {
		t.Errorf("want a clean result, got %v unchecked %v", summary(result.Diagnostics), names(result.Unchecked))
	}
}

func TestValidateReportsAProjectLevelFindingInTheDeclaringFile(t *testing.T) {
	t.Parallel()
	result := validate(t, postgresProject(t, projectText("ghost", timeoutOverride), nil))
	want := []found{{"postgres.unknown_owner", "stackpuddle.yaml", "/spec/modules/db/options/databases/application/owner"}}
	if got := summary(result.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Errorf("findings (the override of another option must not repeat it):\nwant %v\ngot  %v", want, got)
	}
	if _, _, hasLine := result.Diagnostics[0].Location().Position(); !hasLine {
		t.Error("the finding has no line")
	}
}

func TestValidateReportsAnOverrideIntroducedFindingOnceAtTheOverride(t *testing.T) {
	t.Parallel()
	result := validate(t, postgresProject(t, projectText("migrator", ghostOverride), nil))
	want := []found{{"postgres.unknown_owner", "stackpuddle.yaml", "/spec/environments/local/overrides/modules/db/options/databases/application/owner"}}
	if got := summary(result.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Errorf("findings:\nwant %v\ngot  %v", want, got)
	}
}

func TestValidateStopsAtSchemaViolationsOfTheEffectiveOptions(t *testing.T) {
	t.Parallel()
	// The defaults file is not checked alone, so a bad default first shows in the merged options.
	badDefaults := "startupTimeoutSeconds: 0\n"
	result := validate(t, postgresProject(t, projectText("ghost", plainEnvironment), map[string]string{"content/configuration/defaults.yaml": badDefaults}))
	want := []found{{"structure.invalid_value", "content/configuration/defaults.yaml", "/startupTimeoutSeconds"}}
	if got := summary(result.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Errorf("findings (semantic rules must not run on schema-invalid options):\nwant %v\ngot  %v", want, got)
	}
}

func TestValidateListsUnresolvedDefinitionsAsUnchecked(t *testing.T) {
	t.Parallel()
	project := loadProject(t, os.DirFS(filepath.Join(conformanceAt, "valid", "unresolved-packaged")), "stackpuddle.yaml")
	result := validate(t, project)
	if got, want := names(result.Unchecked), []string{"postgres"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Unchecked = %v, want %v", got, want)
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("an unresolved definition produced %v", summary(result.Diagnostics))
	}
}

const uncataloguedModule = `apiVersion: stackpuddle/v1
kind: Module
name: audit-store
version: 0.1.0
configuration:
  schema: content/options.schema.json
`

func TestValidateListsAPackagedDefinitionWithoutAValidatorAsUnchecked(t *testing.T) {
	t.Parallel()
	definition := loadDefinition(t, fstest.MapFS{
		"module.yaml":                 {Data: []byte(uncataloguedModule)},
		"content/options.schema.json": {Data: []byte(`{"type": "object"}`)},
	})
	text := "apiVersion: stackpuddle/v1\nkind: Project\nmetadata: {name: shop}\nspec:\n  modules:\n    audit: {definition: audit-store, options: {}}\n    other: {definition: audit-store, options: {}}\n"
	project := loadProject(t, fstest.MapFS{"stackpuddle.yaml": {Data: []byte(text)}}, "stackpuddle.yaml", definition)
	result := validate(t, project)
	if got, want := names(result.Unchecked), []string{"audit-store"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Unchecked = %v, want one unique %v", got, want)
	}
}

func TestValidateChecksAProjectLocalDefinitionBySchemaOnly(t *testing.T) {
	t.Parallel()
	moduleText := strings.Replace(uncataloguedModule, "audit-store", "postgres", 1) + "  defaults: content/defaults.yaml\n"
	files := fstest.MapFS{
		"stackpuddle.yaml":                          {Data: []byte("apiVersion: stackpuddle/v1\nkind: Project\nmetadata: {name: shop}\nspec:\n  definitions:\n    postgres: {path: defs/postgres}\n  modules:\n    db: {definition: postgres, options: {n: 1, owner: ghost}}\n")},
		"defs/postgres/module.yaml":                 {Data: []byte(moduleText)},
		"defs/postgres/content/options.schema.json": {Data: []byte(`{"type": "object", "properties": {"n": {"type": "integer"}, "m": {"type": "integer"}}}`)},
		"defs/postgres/content/defaults.yaml":       {Data: []byte("m: not-a-number\n")},
	}
	result := validate(t, loadProject(t, files, "stackpuddle.yaml"))
	want := []found{{"structure.invalid_type", "defs/postgres/content/defaults.yaml", "/m"}}
	if got := summary(result.Diagnostics); !reflect.DeepEqual(got, want) {
		t.Errorf("findings (a local definition gets the schema check, not the catalogued rules):\nwant %v\ngot  %v", want, got)
	}
	if len(result.Unchecked) != 0 {
		t.Errorf("a local definition is not unchecked: %v", names(result.Unchecked))
	}
}

func TestValidateSortsItsDiagnostics(t *testing.T) {
	t.Parallel()
	text := projectText("migrator", ghostOverride)
	text = strings.Replace(text, "memberOf: [writer]", "memberOf: [writer, missing]", 1)
	result := validate(t, postgresProject(t, text, nil))
	if len(result.Diagnostics) != 2 {
		t.Fatalf("want several findings, got %v", summary(result.Diagnostics))
	}
	if sorted := result.Diagnostics.Sorted(); !slices.EqualFunc(sorted, result.Diagnostics, func(a, b model.Diagnostic) bool {
		return a.Code() == b.Code() && a.Location() == b.Location()
	}) {
		t.Errorf("diagnostics are not sorted: %v", summary(result.Diagnostics))
	}
}
