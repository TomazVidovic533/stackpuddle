package conformance_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/validation"
)

// runSemanticsCase loads the project, which must be clean, then runs the module validators over it
// and compares the sorted diagnostics with expected/diagnostics.json. A valid case lists "[]".
func runSemanticsCase(t *testing.T, dir caseDir) {
	t.Helper()
	diagnostics, unchecked := semanticsOutcome(t, dir)
	if len(unchecked) > 0 {
		t.Fatalf("definitions left unchecked: %v", unchecked)
	}
	data, ok := readExpected(t, dir, expectedDiags)
	if !ok {
		t.Fatal("a semantics case needs expected/diagnostics.json, \"[]\" when it is valid")
	}
	assertCanonicalASCII(t, expectedDiags, data)
	var want []expectedDiagnostic
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("decode expected/%s: %v", expectedDiags, err)
	}
	got := make([]expectedDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		line, column, _ := diagnostic.Location().Position()
		got = append(got, expectedDiagnostic{
			Code: string(diagnostic.Code()), File: diagnostic.Location().File(), Line: line, Column: column,
			Pointer: diagnostic.Location().Pointer(),
		})
	}
	if want == nil {
		want = []expectedDiagnostic{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics differ\nwant %+v\ngot  %+v\nmessages: %v", want, got, describe(diagnostics))
	}
}

// semanticsOutcome returns the sorted diagnostics and unchecked definition names of a semantics case.
func semanticsOutcome(t *testing.T, dir caseDir) (model.Diagnostics, []string) {
	t.Helper()
	project, loadDiagnostics := loadCaseProject(t, dir)
	if len(loadDiagnostics) > 0 {
		t.Fatalf("a semantics case must load cleanly: %v", describe(loadDiagnostics))
	}
	result, err := validation.Validate(project)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	var unchecked []string
	for _, name := range result.Unchecked {
		unchecked = append(unchecked, name.String())
	}
	return result.Diagnostics, unchecked
}

// caseDiagnostics returns what a case reports: the loader's diagnostics for a project case, the
// validators' for a semantics case.
func caseDiagnostics(t *testing.T, dir caseDir) model.Diagnostics {
	t.Helper()
	if dir.spec.Subject == "semantics" {
		diagnostics, _ := semanticsOutcome(t, dir)
		return diagnostics
	}
	_, diagnostics := loadCaseProject(t, dir)
	return diagnostics
}
