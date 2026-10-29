package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

func TestPostgresModuleDefinition_LoadsThroughTheGenericContract(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(postgresModuleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	definition, diagnostics := model.LoadModuleDefinition(root.FS())
	if len(diagnostics) > 0 {
		t.Fatalf("modules/postgres does not load: %v", describeModel(diagnostics))
	}
	var requires []string
	for _, capability := range definition.Requires() {
		requires = append(requires, capability.String())
	}
	if !slices.Equal(requires, []string{"module.postgres/1", "runtime.compose/1"}) {
		t.Errorf("requires %v", requires)
	}
	if format, ok := definition.DataFormat(); !ok || format.String() != "postgresql/18" {
		t.Errorf("dataFormat %v, %v; want postgresql/18", format, ok)
	}
	if definition.Name().String() != "postgres" || definition.Version().String() != "0.1.0-alpha.1" || definition.Origin() != model.OriginPackaged {
		t.Errorf("identity %s %s %s", definition.Name(), definition.Version(), definition.Origin())
	}
	if _, ok := definition.Defaults(); !ok {
		t.Error("the module's defaults are not loaded")
	}
}

// TestPostgresModuleDefinition_SchemasPublishNoCLIRange holds compatibility by contract: no CLI
// version range exists until releases are tagged (ADR-0007).
func TestPostgresModuleDefinition_SchemasPublishNoCLIRange(t *testing.T) {
	t.Parallel()
	rangeLike := regexp.MustCompile(`(?i)cli|range|compatib`)
	for _, stem := range []string{"module", "bundle"} {
		for _, property := range schemaProperties(readPublishedSchema(t, stem)) {
			if rangeLike.MatchString(property) {
				t.Errorf("%s schema defines %q", stem, property)
			}
		}
	}
}

func loadPostgresDefinition(t *testing.T) model.ModuleDefinition {
	t.Helper()
	definition, diagnostics := model.LoadModuleDefinition(os.DirFS(postgresModuleRoot(t)))
	if len(diagnostics) > 0 {
		t.Fatalf("modules/postgres does not load: %v", describeModel(diagnostics))
	}
	return definition
}

// TestPostgresModuleDefinition_ValidatesEffectiveOptions checks the entry point the configuration
// layer calls with merged options: pointers are relative to the options and carry no file.
func TestPostgresModuleDefinition_ValidatesEffectiveOptions(t *testing.T) {
	t.Parallel()
	definition := loadPostgresDefinition(t)
	valid, err := json.Marshal(readPostgresDocument(t, filepath.Join("testdata", "modules", "postgres", "valid", "minimal.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := definition.ValidateOptions(valid); len(diagnostics) > 0 {
		t.Fatalf("valid options refused: %v", describeModel(diagnostics))
	}
	invalid := []byte(`{"users": {"app": {"purpose": "admin", "passwordSecretRef": "app-password"}}, "roles": {"writer": {"login": false}},
		"databases": {"shop": {"owner": "app", "connectRoles": ["writer"], "schemas": {"public": {"grants": {"writer": {"tables": ["SELECT"]}}}}}}}`)
	diagnostics := definition.ValidateOptions(invalid)
	if len(diagnostics) != 1 || diagnostics[0].Code() != model.StructureInvalidValue ||
		diagnostics[0].Location().Pointer() != "/users/app/purpose" || diagnostics[0].Location().File() != "" {
		t.Fatalf("want one invalid value at /users/app/purpose, got %v", describeModel(diagnostics))
	}
	if diagnostics := definition.ValidateOptions([]byte(`[]`)); len(diagnostics) != 1 || diagnostics[0].Code() != model.StructureInvalidType {
		t.Fatalf("options that are not an object: got %v", describeModel(diagnostics))
	}
}

// TestPostgresModuleDefinition_RequirementsReachTheProject checks what an engine must advertise
// for a project that uses the module: the runtime, the secret provider and the module's needs.
func TestPostgresModuleDefinition_RequirementsReachTheProject(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(filepath.Join(conformanceCases, "valid", "postgres-instance"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	project, diagnostics := model.Load(root.FS(), "stackpuddle.yaml", []model.ModuleDefinition{loadPostgresDefinition(t)})
	if len(diagnostics) > 0 {
		t.Fatalf("load: %v", describeModel(diagnostics))
	}
	local, err := model.ParseEnvironmentName("local")
	if err != nil {
		t.Fatal(err)
	}
	capabilities, ok := project.RequiredCapabilities(local)
	var names []string
	for _, capability := range capabilities {
		names = append(names, capability.String())
	}
	if !ok || !slices.Equal(names, []string{"module.postgres/1", "runtime.compose/1", "secrets.local/1"}) {
		t.Fatalf("required capabilities %v, %v", names, ok)
	}
	missing, err := model.ParseEnvironmentName("staging")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := project.RequiredCapabilities(missing); ok {
		t.Fatal("an undeclared environment has no requirements")
	}
}
