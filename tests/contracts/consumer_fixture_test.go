package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/validation"
)

// fixtureDir is the shared consumer fixture other repositories' tests read from this tree.
func fixtureDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "fixtures", "consumer"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestConsumerFixture_LoadsWithThePackagedPostgresDefinitionAndNoDiagnostics(t *testing.T) {
	t.Parallel()
	definition := loadPostgresDefinition(t)
	project, diagnostics := model.Load(os.DirFS(fixtureDir(t)), "stackpuddle.yaml", []model.ModuleDefinition{definition})
	if len(diagnostics) > 0 {
		t.Fatalf("the consumer fixture does not load: %v", describeModel(diagnostics))
	}
	result, err := validation.Validate(project)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(result.Diagnostics) > 0 || len(result.Unchecked) > 0 {
		t.Fatalf("the consumer fixture does not validate: %v, unchecked %v", describeModel(result.Diagnostics), result.Unchecked)
	}
}

func TestConsumerFixture_HoldsTheMigrationAndRuntimeIdentitiesAndTheTwoEnvironments(t *testing.T) {
	t.Parallel()
	project, diagnostics := model.Load(os.DirFS(fixtureDir(t)), "stackpuddle.yaml", []model.ModuleDefinition{loadPostgresDefinition(t)})
	if len(diagnostics) > 0 {
		t.Fatalf("the consumer fixture does not load: %v", describeModel(diagnostics))
	}
	var consumers []string
	for _, b := range project.Bindings() {
		parameters, _ := b.Parameters()
		var compact bytes.Buffer
		if err := json.Compact(&compact, parameters); err != nil {
			t.Fatalf("binding parameters are not JSON: %v", err)
		}
		consumers = append(consumers, b.Ref().Consumer.String()+" "+compact.String())
	}
	slices.Sort(consumers)
	want := []string{
		`workload/app {"database":"app","user":"app_runtime"}`,
		`workload/migrate {"database":"app","user":"app_migrator"}`,
	}
	if !slices.Equal(consumers, want) {
		t.Errorf("bindings = %v, want %v", consumers, want)
	}
	var environments []string
	for _, env := range project.Environments() {
		environments = append(environments, env.Name().String())
	}
	slices.Sort(environments)
	if !slices.Equal(environments, []string{"failing-migration", "local"}) {
		t.Errorf("environments = %v", environments)
	}
	app := false
	for _, w := range project.Workloads() {
		if w.Key().String() == "app" {
			app = len(w.Prerequisites()) == 1 && w.Prerequisites()[0].String() == "migrate"
		}
	}
	if !app {
		t.Error("app does not require the migrate job")
	}
}
