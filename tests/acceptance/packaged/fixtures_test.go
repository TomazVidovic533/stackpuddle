package packaged_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

// materialize copies a testdata project into a temp directory with the harness placeholders filled in.
func materialize(t *testing.T, root string) string {
	t.Helper()
	dest := t.TempDir()
	placeholders := strings.NewReplacer("{{NAME}}", "fixture", "{{SLOW}}", "1")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(placeholders.Replace(string(data))), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dest
}

// A journey that expects one specific refusal (exit 7, a missing image) must not be refused earlier because its
// fixture is malformed, so every acceptance project has to load as a valid model.
func TestFixtures_EachAcceptanceProjectLoadsAsAValidModel(t *testing.T) {
	for _, name := range []string{"build", "groups", filepath.Join("compat", "capability")} {
		t.Run(name, func(t *testing.T) {
			dir := materialize(t, filepath.Join("testdata", name))
			if _, diags := model.Load(os.DirFS(dir), "stackpuddle.yaml", nil); len(diags) > 0 {
				t.Fatalf("testdata/%s does not load: %v", name, diags)
			}
		})
	}
}

func TestFixtures_TheCompatibilityProjectHasADefaultApplicationSoUpHasSomethingToSelect(t *testing.T) {
	dir := materialize(t, filepath.Join("testdata", "compat", "capability"))
	project, diags := model.Load(os.DirFS(dir), "stackpuddle.yaml", nil)
	if len(diags) > 0 {
		t.Fatalf("does not load: %v", diags)
	}
	defaults := 0
	for _, application := range project.Applications() {
		if application.Default() && len(application.Workloads()) > 0 {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("default applications with a workload = %d, want 1", defaults)
	}
}
