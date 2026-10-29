package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const bindingProject = `apiVersion: stackpuddle/v1
kind: Project
metadata:
  name: shop
spec:
  workloads:
    api:
      type: service
      image: ghcr.io/example/api:1.4.2
      listeners:
        http: {port: 8080, protocol: http}
      exports:
        public: {capability: http.api/1, listener: http}
        internal: {capability: http.api/1, listener: http}
    web:
      type: service
      image: ghcr.io/example/web:3.0.0
      dependencies:
        api: {capability: http.api/1}
    smoke:
      type: job
      image: ghcr.io/example/smoke:1.0.0
      dependencies:
        api: {capability: http.api/1}
    zeta:
      type: service
      image: ghcr.io/example/zeta:1.0.0
      dependencies:
        api: {capability: http.api/1}
  bindings:
    - {consumer: workload/zeta, dependency: api, source: {object: workload/api, export: public}}
    - {consumer: workload/web, dependency: api, source: {object: workload/api, export: SOURCE}}
    - {consumer: workload/smoke, dependency: api, source: {object: workload/api, export: public}}
`

func loadBindingProject(t *testing.T, source string) model.Project {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stackpuddle.yaml"), []byte(strings.ReplaceAll(bindingProject, "SOURCE", source)), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	project, diagnostics := model.Load(root.FS(), "stackpuddle.yaml", nil)
	if len(diagnostics) > 0 {
		t.Fatalf("load: %v", diagnostics)
	}
	return project
}

// encodedBindingIndex returns where the web binding sits in the canonical model's bindings array.
func encodedBindingIndex(t *testing.T, project model.Project) int {
	t.Helper()
	var document struct {
		Spec struct {
			Bindings []struct {
				Consumer string `json:"consumer"`
			} `json:"bindings"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(model.Encode(project), &document); err != nil {
		t.Fatal(err)
	}
	for index, binding := range document.Spec.Bindings {
		if binding.Consumer == "workload/web" {
			return index
		}
	}
	t.Fatal("the web binding is not in the encoded model")
	return -1
}

func TestChangingABindingsSourceKeepsItsIdentityAndPosition(t *testing.T) {
	t.Parallel()
	before, after := loadBindingProject(t, "public"), loadBindingProject(t, "internal")
	find := func(project model.Project) model.Binding {
		for _, binding := range project.Bindings() {
			if binding.Ref().Consumer.String() == "workload/web" {
				return binding
			}
		}
		t.Fatal("no web binding")
		return model.Binding{}
	}
	beforeBinding, afterBinding := find(before), find(after)
	if beforeBinding.Ref() != afterBinding.Ref() || beforeBinding.Ref().String() != "binding/workload/web/api" {
		t.Fatalf("identity changed: %s then %s", beforeBinding.Ref(), afterBinding.Ref())
	}
	if beforeBinding.Source() == afterBinding.Source() {
		t.Fatal("the two projects must differ in the binding's source")
	}
	if encodedBindingIndex(t, before) != encodedBindingIndex(t, after) {
		t.Fatal("the binding moved in the canonical model when only its source changed")
	}
	if _, ok := before.Provenance(mustRef(t, "binding/workload/web/api")); !ok {
		t.Fatal("the binding reference must name the binding's provenance")
	}
}

func mustRef(t *testing.T, text string) model.Ref {
	t.Helper()
	ref, err := model.ParseRef(text)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
