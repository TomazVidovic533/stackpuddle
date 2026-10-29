package conformance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const (
	casesDir        = "cases"
	repositoryRoot  = "../.."
	roundTripName   = "conformance-round-trip.json"
	expectedDir     = "expected"
	expectedModel   = "model.json"
	expectedDiags   = "diagnostics.json"
	expectedBinds   = "bindings.json"
	expectedResolve = "resolution-diagnostics.json"
	expectedUnres   = "unresolved.json"
)

// conformanceCase is one case.json; tests/conformance/README.md documents every member.
type conformanceCase struct {
	Description  string             `json:"description"`
	Subject      string             `json:"subject"`
	Root         string             `json:"root"`
	Bundle       string             `json:"bundle"`
	Packaged     []packagedSource   `json:"packaged"`
	Environment  string             `json:"environment"`
	Addresses    []addressFixture   `json:"addresses"`
	EquivalentTo string             `json:"equivalentTo"`
	Parity       *parityExpectation `json:"parity"`
}

type packagedSource struct {
	Repository string `json:"repository"`
	Case       string `json:"case"`
}

type addressFixture struct {
	Source struct {
		Object string `json:"object"`
		Export string `json:"export"`
	} `json:"source"`
	Context string `json:"context"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type parityExpectation struct {
	SDK     string        `json:"sdk"`
	Patches []parityPatch `json:"patches"`
}

type parityPatch struct {
	Ref    string   `json:"ref"`
	Fields []string `json:"fields"`
}

// expectedDiagnostic is one entry of an expected diagnostics file. Messages are not contract.
type expectedDiagnostic struct {
	Code    string `json:"code"`
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Pointer string `json:"pointer"`
}

type caseDir struct {
	name string
	path string
	spec conformanceCase
}

func TestConformanceCases(t *testing.T) {
	for _, dir := range listCases(t) {
		t.Run(dir.name, func(t *testing.T) {
			t.Parallel()
			switch dir.spec.Subject {
			case "project":
				runProjectCase(t, dir)
			case "module":
				runModuleCase(t, dir)
			case "bundle-lock":
				runBundleLockCase(t, dir)
			case "version-info":
				runVersionInfoCase(t, dir)
			case "versions":
				runVersionsCase(t, dir)
			case "semantics":
				runSemanticsCase(t, dir)
			default:
				t.Fatalf("unknown subject %q", dir.spec.Subject)
			}
		})
	}
}

func listCases(t *testing.T) []caseDir {
	t.Helper()
	var dirs []caseDir
	for _, group := range []string{"valid", "invalid", "parity", "semantics"} {
		entries, err := os.ReadDir(filepath.Join(casesDir, group))
		if err != nil {
			t.Fatalf("read case group %s: %v", group, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				t.Fatalf("unexpected file %s in case group %s", entry.Name(), group)
			}
			name := group + "/" + entry.Name()
			path := filepath.Join(casesDir, group, entry.Name())
			dirs = append(dirs, caseDir{name: name, path: path, spec: readCaseSpec(t, path)})
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no conformance cases found")
	}
	return dirs
}

func readCaseSpec(t *testing.T, path string) conformanceCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(path, "case.json"))
	if err != nil {
		t.Fatalf("read case.json: %v", err)
	}
	var spec conformanceCase
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		t.Fatalf("decode %s/case.json: %v", path, err)
	}
	if spec.Description == "" {
		t.Fatalf("%s/case.json has no description", path)
	}
	return spec
}

func openRoot(t *testing.T, path string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatalf("open root %s: %v", path, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func loadPackaged(t *testing.T, dir caseDir) []model.ModuleDefinition {
	t.Helper()
	var definitions []model.ModuleDefinition
	for _, source := range dir.spec.Packaged {
		path := filepath.Join(dir.path, filepath.FromSlash(source.Case))
		if source.Repository != "" {
			path = filepath.Join(repositoryRoot, "modules", source.Repository)
		}
		definition, diagnostics := model.LoadModuleDefinition(openRoot(t, path).FS())
		if len(diagnostics) > 0 {
			t.Fatalf("packaged definition %s does not load: %v", path, describe(diagnostics))
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func loadCaseProject(t *testing.T, dir caseDir) (model.Project, model.Diagnostics) {
	t.Helper()
	return model.Load(openRoot(t, dir.path).FS(), dir.spec.Root, loadPackaged(t, dir))
}

func runProjectCase(t *testing.T, dir caseDir) {
	project, diagnostics := loadCaseProject(t, dir)
	if expectDiagnostics(t, dir, expectedDiags, diagnostics) {
		return
	}
	encoded := model.Encode(project)
	want, hasModel := readExpected(t, dir, expectedModel)
	switch {
	case dir.spec.Parity != nil && len(dir.spec.Parity.Patches) > 0:
		checkPatchedParity(t, dir, encoded, want)
	case hasModel:
		if !bytes.Equal(encoded, want) {
			t.Errorf("Encode differs from expected/model.json:\n%s", lineDiff(string(want), string(encoded)))
		}
	default:
		t.Fatal("a valid project case needs expected/model.json")
	}
	checkModelRoot(t, dir, want)
	checkRoundTrip(t, dir, encoded)
	checkUnresolved(t, dir, project)
	checkEquivalence(t, dir, encoded)
	checkResolution(t, dir, project)
}

// expectDiagnostics compares diagnostics with the expected file; it reports whether the case
// expected any, in which case nothing else about it is checked.
func expectDiagnostics(t *testing.T, dir caseDir, name string, diagnostics model.Diagnostics) bool {
	t.Helper()
	data, ok := readExpected(t, dir, name)
	if !ok {
		if len(diagnostics) > 0 {
			t.Fatalf("expected no diagnostics, got %v", describe(diagnostics))
		}
		return false
	}
	assertCanonicalASCII(t, name, data)
	var want []expectedDiagnostic
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("decode expected/%s: %v", name, err)
	}
	if len(want) == 0 {
		t.Fatalf("expected/%s lists no diagnostics; delete it instead", name)
	}
	got := make([]expectedDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics.Sorted() {
		line, column, _ := diagnostic.Location().Position()
		got = append(got, expectedDiagnostic{
			Code: string(diagnostic.Code()), File: diagnostic.Location().File(), Line: line, Column: column,
			Pointer: diagnostic.Location().Pointer(),
		})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics differ\nwant %+v\ngot  %+v\nmessages: %v", want, got, describe(diagnostics))
	}
	return true
}

// assertCanonicalASCII checks the layout of an expected file whose keys and values are ASCII, for
// which encoding/json's sorted, two-space indented output is the canonical form.
func assertCanonicalASCII(t *testing.T, name string, data []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode expected/%s: %v", name, err)
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		t.Fatalf("encode expected/%s: %v", name, err)
	}
	if !bytes.Equal(canonical.Bytes(), data) {
		t.Errorf("expected/%s is not in canonical layout:\n%s", name, lineDiff(canonical.String(), string(data)))
	}
}

func readExpected(t *testing.T, dir caseDir, name string) ([]byte, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir.path, expectedDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read expected/%s: %v", name, err)
	}
	return data, true
}

// checkModelRoot loads the hand-written expected model as a Model root next to the case's inputs:
// it must be schema-valid, load without diagnostics and encode to itself.
func checkModelRoot(t *testing.T, dir caseDir, want []byte) {
	t.Helper()
	project, diagnostics := model.Load(withModel(t, dir, want), roundTripName, loadPackaged(t, dir))
	if len(diagnostics) > 0 {
		t.Fatalf("expected/model.json does not load as a Model root: %v", describe(diagnostics))
	}
	if again := model.Encode(project); !bytes.Equal(again, want) {
		t.Errorf("expected/model.json is not in canonical form:\n%s", lineDiff(string(want), string(again)))
	}
}

// checkRoundTrip loads the encoder's own output and requires the same bytes back.
func checkRoundTrip(t *testing.T, dir caseDir, encoded []byte) {
	t.Helper()
	project, diagnostics := model.Load(withModel(t, dir, encoded), roundTripName, loadPackaged(t, dir))
	if len(diagnostics) > 0 {
		t.Fatalf("the encoded model does not load: %v", describe(diagnostics))
	}
	if again := model.Encode(project); !bytes.Equal(again, encoded) {
		t.Errorf("Encode(Load(Encode(Load(yaml)))) differs:\n%s", lineDiff(string(encoded), string(again)))
	}
}

func checkUnresolved(t *testing.T, dir caseDir, project model.Project) {
	t.Helper()
	var got []string
	for _, name := range project.Unresolved() {
		got = append(got, name.String())
	}
	data, ok := readExpected(t, dir, expectedUnres)
	if !ok {
		if len(got) > 0 {
			t.Fatalf("unexpected unresolved definitions %v", got)
		}
		return
	}
	assertCanonicalASCII(t, expectedUnres, data)
	var want []string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("decode expected/%s: %v", expectedUnres, err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unresolved definitions: want %v, got %v", want, got)
	}
}

func checkEquivalence(t *testing.T, dir caseDir, encoded []byte) {
	t.Helper()
	if dir.spec.EquivalentTo == "" {
		return
	}
	otherPath := filepath.Join(casesDir, filepath.FromSlash(dir.spec.EquivalentTo))
	other := caseDir{name: dir.spec.EquivalentTo, path: otherPath, spec: readCaseSpec(t, otherPath)}
	project, diagnostics := loadCaseProject(t, other)
	if len(diagnostics) > 0 {
		t.Fatalf("equivalent case %s does not load: %v", other.name, describe(diagnostics))
	}
	if got, want := intent(t, encoded), intent(t, model.Encode(project)); !bytes.Equal(got, want) {
		t.Errorf("intent differs from %s:\n%s", other.name, lineDiff(string(want), string(got)))
	}
}

// intent is a Model without its provenance member: what parity and equivalence compare.
func intent(t *testing.T, encoded []byte) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode model: %v", err)
	}
	delete(document, "provenance")
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("encode intent: %v", err)
	}
	return data
}

// checkPatchedParity checks the YAML half of an SDK patch case: the expected model is the state
// after the SDK patch, so outside the patched fields it must equal what Go loads from the YAML, and
// its provenance must keep the YAML declaration and record the patch.
func checkPatchedParity(t *testing.T, dir caseDir, encoded, want []byte) {
	t.Helper()
	var loaded, expected map[string]any
	if err := json.Unmarshal(encoded, &loaded); err != nil {
		t.Fatalf("decode encoded model: %v", err)
	}
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatalf("decode expected/model.json: %v", err)
	}
	for _, patch := range dir.spec.Parity.Patches {
		kind, key, _ := strings.Cut(patch.Ref, "/")
		target, source := objectSpec(t, loaded, kind, key), objectSpec(t, expected, kind, key)
		for _, field := range patch.Fields {
			name := strings.TrimPrefix(field, "/spec/")
			value, ok := source[name]
			if !ok {
				t.Fatalf("expected model lacks patched field %s of %s", field, patch.Ref)
			}
			target[name] = value
		}
		entry := mapAt(t, expected, "provenance", patch.Ref)
		declared := mapAt(t, loaded, "provenance", patch.Ref)["declared"]
		if !reflect.DeepEqual(entry["declared"], declared) {
			t.Errorf("provenance of %s: declared %v, want the YAML declaration %v", patch.Ref, entry["declared"], declared)
		}
		patched, _ := entry["patched"].([]any)
		if len(patched) == 0 {
			t.Errorf("provenance of %s records no patch", patch.Ref)
		}
	}
	delete(loaded, "provenance")
	delete(expected, "provenance")
	if !reflect.DeepEqual(loaded, expected) {
		t.Errorf("outside the patched fields the expected model differs from the YAML")
	}
}

func objectSpec(t *testing.T, document map[string]any, kind, key string) map[string]any {
	t.Helper()
	kinds := map[string]string{"workload": "Workload", "module": "ModuleInstance", "client": "Client"}
	objects, _ := mapAt(t, document, "spec")["objects"].([]any)
	for _, item := range objects {
		object, _ := item.(map[string]any)
		if object["kind"] == kinds[kind] && object["key"] == key {
			spec, _ := object["spec"].(map[string]any)
			return spec
		}
	}
	t.Fatalf("model has no %s/%s", kind, key)
	return nil
}

func mapAt(t *testing.T, document map[string]any, keys ...string) map[string]any {
	t.Helper()
	current := document
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("model has no object at %s", strings.Join(keys, "/"))
		}
		current = next
	}
	return current
}

func checkResolution(t *testing.T, dir caseDir, project model.Project) {
	t.Helper()
	if len(dir.spec.Addresses) == 0 {
		return
	}
	environment, err := model.ParseEnvironmentName(dir.spec.Environment)
	if err != nil {
		t.Fatalf("case environment: %v", err)
	}
	addresses := map[string]model.Endpoint{}
	for _, address := range dir.spec.Addresses {
		addresses[address.Source.Object+"#"+address.Source.Export+"@"+address.Context] = model.Endpoint{Host: address.Host, Port: address.Port}
	}
	lookup := func(source model.ExportRef, context model.Context) (model.Endpoint, bool) {
		endpoint, ok := addresses[source.Object.String()+"#"+source.Export.String()+"@"+context.String()]
		return endpoint, ok
	}
	bindings, diagnostics := project.ResolveBindings(environment, lookup)
	if expectDiagnostics(t, dir, expectedResolve, diagnostics) {
		return
	}
	want, ok := readExpected(t, dir, expectedBinds)
	if !ok {
		t.Fatal("a case with addresses needs expected/bindings.json or expected/resolution-diagnostics.json")
	}
	if got := model.EncodeBindings(bindings); !bytes.Equal(got, want) {
		t.Errorf("EncodeBindings differs from expected/bindings.json:\n%s", lineDiff(string(want), string(got)))
	}
}

func runModuleCase(t *testing.T, dir caseDir) {
	path := filepath.Join(dir.path, filepath.FromSlash(dir.spec.Root))
	if dir.spec.Root == "" {
		if len(dir.spec.Packaged) != 1 || dir.spec.Packaged[0].Repository == "" {
			t.Fatal("a module case names a root directory or one packaged repository module")
		}
		path = filepath.Join(repositoryRoot, "modules", dir.spec.Packaged[0].Repository)
	}
	_, diagnostics := model.LoadModuleDefinition(openRoot(t, path).FS())
	expectDiagnostics(t, dir, expectedDiags, diagnostics)
}

func runBundleLockCase(t *testing.T, dir caseDir) {
	fsys := openRoot(t, dir.path).FS()
	lock, lockDiagnostics := model.LoadLock(fsys, dir.spec.Root)
	bundle, bundleDiagnostics := model.LoadBundle(fsys, dir.spec.Bundle)
	diagnostics := slices.Concat(lockDiagnostics, bundleDiagnostics)
	if len(diagnostics) == 0 {
		diagnostics = model.CheckLock(lock, bundle)
	}
	expectDiagnostics(t, dir, expectedDiags, diagnostics)
}

func runVersionInfoCase(t *testing.T, dir caseDir) {
	data, err := os.ReadFile(filepath.Join(dir.path, filepath.FromSlash(dir.spec.Root)))
	if err != nil {
		t.Fatalf("read version info: %v", err)
	}
	info, diagnostics := model.ParseVersionInfo(data)
	if expectDiagnostics(t, dir, expectedDiags, diagnostics) {
		return
	}
	got, err := model.EncodeVersionInfo(info)
	if err != nil {
		t.Fatalf("EncodeVersionInfo(ParseVersionInfo(file)): %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("EncodeVersionInfo(ParseVersionInfo(file)) differs from the file:\n%s", lineDiff(string(data), string(got)))
	}
}

func runVersionsCase(t *testing.T, dir caseDir) {
	data, err := os.ReadFile(filepath.Join(dir.path, filepath.FromSlash(dir.spec.Root)))
	if err != nil {
		t.Fatalf("read versions: %v", err)
	}
	var texts []string
	if err := json.Unmarshal(data, &texts); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	versions := make([]model.Version, len(texts))
	for index, text := range texts {
		version, err := model.ParseVersion(text)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", text, err)
		}
		versions[index] = version
	}
	for i := range versions {
		for j := range versions {
			want := compareIndexes(i, j)
			if got := versions[i].Compare(versions[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", texts[i], texts[j], got, want)
			}
		}
	}
}

func compareIndexes(i, j int) int {
	switch {
	case i < j:
		return -1
	case i > j:
		return 1
	default:
		return 0
	}
}

func describe(diagnostics model.Diagnostics) []string {
	var lines []string
	for _, diagnostic := range diagnostics.Sorted() {
		line, column, _ := diagnostic.Location().Position()
		lines = append(lines, string(diagnostic.Code())+" "+diagnostic.Location().File()+":"+strconv.Itoa(line)+":"+strconv.Itoa(column)+" "+
			diagnostic.Location().Pointer()+" "+diagnostic.Message())
	}
	return lines
}

// lineDiff shows the first differing line of two texts with its neighbours.
func lineDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for index := 0; index < len(wantLines) || index < len(gotLines); index++ {
		var wantLine, gotLine string
		if index < len(wantLines) {
			wantLine = wantLines[index]
		}
		if index < len(gotLines) {
			gotLine = gotLines[index]
		}
		if wantLine != gotLine {
			return "line " + strconv.Itoa(index+1) + "\nwant: " + wantLine + "\ngot:  " + gotLine
		}
	}
	return "(no line differs)"
}

// withModel copies the case into a fresh directory and adds data at its root, so a Model encoded
// from the case resolves its relative paths against the same inputs, read as real files.
func withModel(t *testing.T, dir caseDir, data []byte) fs.FS {
	t.Helper()
	copyDir := t.TempDir()
	if err := os.CopyFS(copyDir, os.DirFS(dir.path)); err != nil {
		t.Fatalf("copy case: %v", err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, roundTripName), data, 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	return openRoot(t, copyDir).FS()
}
