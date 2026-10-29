package model_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const (
	fuzzRoot      = "stackpuddle.yaml"
	fuzzModelRoot = "model.json"
	fuzzFileLimit = 1 << 20
)

// FuzzLoad feeds arbitrary bytes to Load as a project root. Load must never panic, must refuse an
// oversized file by name, and anything it accepts must encode to a Model that loads back to the
// same bytes.
func FuzzLoad(f *testing.F) {
	addConformanceSeeds(f)
	f.Add([]byte("apiVersion: stackpuddle/v1\nkind: Project\nmetadata: {name: shop}\nspec: {}\n"))
	f.Add([]byte("{apiVersion: stackpuddle/v1, kind: Model, metadata: {name: shop}, spec: {objects: []}}"))
	f.Add([]byte("a: &x [*x]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		project, diagnostics := model.Load(fstest.MapFS{fuzzRoot: {Data: data}}, fuzzRoot, nil)
		if len(data) > fuzzFileLimit {
			if len(diagnostics) != 1 || diagnostics[0].Code() != model.SyntaxLimitExceeded {
				t.Fatalf("an oversized file must fail with %s, got %v", model.SyntaxLimitExceeded, diagnostics)
			}
			return
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Code() == "" || diagnostic.Message() == "" {
				t.Fatalf("a diagnostic without code or message: %+v", diagnostic)
			}
		}
		if len(diagnostics) > 0 {
			return
		}
		encoded := model.Encode(project)
		again, diagnostics := model.Load(fstest.MapFS{fuzzModelRoot: {Data: encoded}}, fuzzModelRoot, nil)
		if len(diagnostics) > 0 {
			t.Fatalf("the encoded model does not load: %v\n%s", diagnostics, encoded)
		}
		if !bytes.Equal(model.Encode(again), encoded) {
			t.Fatalf("the encoded model is not stable:\n%s", encoded)
		}
	})
}

func addConformanceSeeds(f *testing.F) {
	f.Helper()
	cases := filepath.Join("..", "..", "conformance", "cases")
	err := filepath.WalkDir(cases, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f.Add(data)
		return nil
	})
	if err != nil {
		f.Fatalf("read conformance seeds: %v", err)
	}
}
