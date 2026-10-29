package postgres_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/TomazVidovic533/stackpuddle/validation/postgres"
)

// FuzzValidate feeds arbitrary bytes to Validate, seeded from the module's valid fixtures. It must
// never panic, and every diagnostic must carry a postgres code and an options-relative pointer.
func FuzzValidate(f *testing.F) {
	f.Add([]byte(baseOptions))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"users": {"a": {"purpose": "migrations"}}, "roles": {"a": {"login": false}}, "databases": {"d": {"owner": "a", "connectRoles": ["a"], "schemas": {}}}}`))
	seeds, err := filepath.Glob(filepath.Join("..", "..", "..", "contracts", "testdata", "modules", "postgres", "valid", "*.yaml"))
	if err != nil || len(seeds) == 0 {
		f.Fatalf("no seed fixtures found: %v", err)
	}
	for _, seed := range seeds {
		data, err := os.ReadFile(seed)
		if err != nil {
			f.Fatalf("read %s: %v", seed, err)
		}
		var tree any
		if err := yaml.Unmarshal(data, &tree); err != nil {
			f.Fatalf("decode %s: %v", seed, err)
		}
		encoded, err := json.Marshal(tree)
		if err != nil {
			f.Fatalf("encode %s: %v", seed, err)
		}
		f.Add(encoded)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		diagnostics, err := postgres.Validate(data)
		if err != nil {
			if diagnostics != nil {
				t.Fatalf("error with diagnostics: %v", diagnostics)
			}
			return
		}
		for _, diagnostic := range diagnostics {
			if len(diagnostic.Code()) < len("postgres.") || diagnostic.Code()[:len("postgres.")] != "postgres." {
				t.Fatalf("code %q is not a postgres code", diagnostic.Code())
			}
			pointer := diagnostic.Location().Pointer()
			if pointer == "" || pointer[0] != '/' || diagnostic.Location().File() != "" {
				t.Fatalf("pointer %q / file %q is not options-relative", pointer, diagnostic.Location().File())
			}
		}
	})
}
