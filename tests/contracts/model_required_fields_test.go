package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/schemas"
)

// requiredRow removes one required member from a valid document and names the one diagnostic the
// reader must report. The schema key is "<stem>#<pointer to the schema object>/<field>".
type requiredRow struct {
	schema  string
	subject string
	dir     string
	file    string
	remove  string
	code    model.Code
	pointer string
}

const (
	subjectProject     = "project"
	subjectModule      = "module"
	subjectBundle      = "bundle"
	subjectLock        = "lock"
	subjectVersionInfo = "version-info"
	subjectBindings    = "bindings"
	// requiredFieldCheckLimit is far above the one violation each row expects.
	requiredFieldCheckLimit = 100
)

func requiredRows() []requiredRow {
	missing := model.StructureMissingField
	project := func(schema, dir, file, remove string) requiredRow {
		return requiredRow{schema: schema, subject: subjectProject, dir: dir, file: file, remove: remove, code: missing, pointer: remove}
	}
	in := func(dir string) func(schema, remove string) requiredRow {
		return func(schema, remove string) requiredRow {
			return project(schema, "valid/"+dir, "stackpuddle.yaml", remove)
		}
	}
	minimal, every, jobs := in("minimal-service"), in("bindings-every-consumer"), in("job-build-volumes-prerequisites")
	overrides, modelRoot := in("inputs-and-overrides"), func(schema, remove string) requiredRow {
		return project(schema, "valid/model-root", "stackpuddle.model.json", remove)
	}
	other := func(subject, dir, file string) func(schema, remove string) requiredRow {
		return func(schema, remove string) requiredRow {
			return requiredRow{schema: schema, subject: subject, dir: dir, file: file, remove: remove, code: missing, pointer: remove}
		}
	}
	lock := other(subjectLock, "valid/bundle-lock", "stackpuddle.lock.yaml")
	bundle := other(subjectBundle, "valid/bundle-lock", "bundle.yaml")
	module := other(subjectModule, "../../../modules/postgres", "module.yaml")
	versionInfo := other(subjectVersionInfo, "valid/version-info-engine", "version-info.json")
	bindings := other(subjectBindings, "valid/bindings-every-consumer", "expected/bindings.json")
	manifest := func(schema, remove string) requiredRow {
		return project(schema, "valid/postgres-instance", "stackpuddle.manifest.yaml", remove)
	}
	fragment := func(schema, remove string) requiredRow {
		return project(schema, "valid/composition-imports", "fragments/backend.yaml", remove)
	}
	exactlyOne := func(row requiredRow, pointer string) requiredRow {
		row.code, row.pointer = model.StructureExactlyOne, pointer
		return row
	}
	return []requiredRow{
		lock("definitions#/$defs/bundleIdentity/version", "/bundle/version"),
		lock("definitions#/$defs/bundleIdentity/source", "/bundle/source"),
		lock("definitions#/$defs/bundleIdentity/digest", "/bundle/digest"),
		lock("definitions#/$defs/moduleArtifact/version", "/modules/postgres/version"),
		lock("definitions#/$defs/moduleArtifact/source", "/modules/postgres/source"),
		lock("definitions#/$defs/moduleArtifact/format", "/modules/postgres/format"),
		lock("definitions#/$defs/moduleArtifact/digest", "/modules/postgres/digest"),
		minimal("definitions#/$defs/projectMetadata/name", "/metadata/name"),
		every("definitions#/$defs/listener/port", "/spec/workloads/api/listeners/http/port"),
		every("definitions#/$defs/listener/protocol", "/spec/workloads/api/listeners/http/protocol"),
		every("definitions#/$defs/exportValue/sensitivity", "/spec/workloads/api/exports/http/values/token/sensitivity"),
		every("definitions#/$defs/export/capability", "/spec/workloads/api/exports/http/capability"),
		every("definitions#/$defs/export/listener", "/spec/workloads/api/exports/http/listener"),
		every("definitions#/$defs/dependency/capability", "/spec/workloads/web/dependencies/api/capability"),
		jobs("definitions#/$defs/volume/path", "/spec/workloads/api/volumes/uploads/path"),
		jobs("definitions#/$defs/build/context", "/spec/workloads/migrate/build/context"),
		exactlyOne(in("composition-single-file")("definitions#/$defs/envValue/oneOf/1/input", "/spec/workloads/api/environment/LOG_LEVEL/input"),
			"/spec/workloads/api/environment/LOG_LEVEL"),
		minimal("definitions#/$defs/workloadSpec/type", "/spec/workloads/api/type"),
		exactlyOne(minimal("definitions#/$defs/workloadSpec/oneOf/0/image", "/spec/workloads/api/image"), "/spec/workloads/api"),
		exactlyOne(jobs("definitions#/$defs/workloadSpec/oneOf/1/build", "/spec/workloads/migrate/build"), "/spec/workloads/migrate"),
		jobs("definitions#/$defs/workloadSpec/properties/prerequisites/items/job", "/spec/workloads/seed/prerequisites/0/job"),
		every("definitions#/$defs/moduleInstanceSpec/definition", "/spec/modules/queue/definition"),
		every("definitions#/$defs/clientSpec/dependencies", "/spec/clients/cli/dependencies"),
		in("applications-overlap")("definitions#/$defs/application/workloads", "/spec/applications/admin/workloads"),
		every("definitions#/$defs/exportRef/object", "/spec/bindings/0/source/object"),
		every("definitions#/$defs/exportRef/export", "/spec/bindings/0/source/export"),
		every("definitions#/$defs/fileDelivery/path", "/spec/bindings/1/delivery/files/token/path"),
		every("definitions#/$defs/fileDelivery/mode", "/spec/bindings/1/delivery/files/token/mode"),
		every("definitions#/$defs/binding/consumer", "/spec/bindings/0/consumer"),
		every("definitions#/$defs/binding/dependency", "/spec/bindings/0/dependency"),
		every("definitions#/$defs/binding/source", "/spec/bindings/0/source"),
		overrides("definitions#/$defs/input/sensitivity", "/spec/inputs/api-token/sensitivity"),
		overrides("definitions#/$defs/inputSources/properties/local/file", "/spec/environments/local/inputs/api-token/local/file"),
		overrides("definitions#/$defs/inputSources/properties/local/key", "/spec/environments/local/inputs/api-token/local/key"),
		minimal("definitions#/$defs/composeRuntime/type", "/spec/environments/local/runtime/type"),
		minimal("definitions#/$defs/composeRuntime/context", "/spec/environments/local/runtime/context"),
		every("definitions#/$defs/exposure/source", "/spec/environments/local/exposures/0/source"),
		minimal("definitions#/$defs/environment/runtime", "/spec/environments/local/runtime"),
		minimal("definitions#/$defs/environment/secrets", "/spec/environments/local/secrets"),
		minimal("definitions#/$defs/environment/properties/secrets/provider", "/spec/environments/local/secrets/provider"),
		overrides("definitions#/$defs/environment/properties/overrides/properties/workloads/additionalProperties/environment",
			"/spec/environments/local/overrides/workloads/api/environment"),
		overrides("definitions#/$defs/environment/properties/overrides/properties/modules/additionalProperties/options",
			"/spec/environments/local/overrides/modules/primary/options"),
		in("custom-definition")("definitions#/$defs/definitionsSection/additionalProperties/path", "/spec/definitions/audit-store/path"),
		modelRoot("definitions#/$defs/origin/origin", "/provenance/workload~1api/declared/origin"),
		modelRoot("definitions#/$defs/provenanceEntry/declared", "/provenance/env~1local/declared"),
		modelRoot("definitions#/$defs/provenanceEntry/properties/patched/items/origin", "/provenance/workload~1api/patched/0/origin"),
		modelRoot("definitions#/$defs/provenanceEntry/properties/patched/items/fields", "/provenance/workload~1api/patched/0/fields"),
		minimal("project#/apiVersion", "/apiVersion"),
		minimal("project#/kind", "/kind"),
		minimal("project#/metadata", "/metadata"),
		minimal("project#/spec", "/spec"),
		fragment("fragment#/apiVersion", "/apiVersion"),
		fragment("fragment#/kind", "/kind"),
		fragment("fragment#/spec", "/spec"),
		modelRoot("model#/apiVersion", "/apiVersion"),
		modelRoot("model#/kind", "/kind"),
		modelRoot("model#/metadata", "/metadata"),
		modelRoot("model#/spec", "/spec"),
		modelRoot("model#/$defs/object/apiVersion", "/spec/objects/0/apiVersion"),
		modelRoot("model#/$defs/object/kind", "/spec/objects/0/kind"),
		modelRoot("model#/$defs/object/key", "/spec/objects/0/key"),
		modelRoot("model#/$defs/object/spec", "/spec/objects/0/spec"),
		module("module#/apiVersion", "/apiVersion"),
		module("module#/kind", "/kind"),
		module("module#/name", "/name"),
		module("module#/version", "/version"),
		module("module#/properties/requires/capabilities", "/requires/capabilities"),
		module("module#/properties/configuration/schema", "/configuration/schema"),
		module("module#/properties/runtimes/properties/compose/file", "/runtimes/compose/file"),
		module("module#/properties/provisioning/contract", "/provisioning/contract"),
		bundle("bundle#/apiVersion", "/apiVersion"),
		bundle("bundle#/kind", "/kind"),
		bundle("bundle#/version", "/version"),
		bundle("bundle#/modules", "/modules"),
		bundle("bundle#/properties/modules/additionalProperties/version", "/modules/postgres/version"),
		bindings("bindings#/apiVersion", "/apiVersion"),
		bindings("bindings#/kind", "/kind"),
		bindings("bindings#/metadata", "/metadata"),
		bindings("bindings#/bindings", "/bindings"),
		bindings("bindings#/properties/metadata/project", "/metadata/project"),
		bindings("bindings#/properties/metadata/environment", "/metadata/environment"),
		bindings("bindings#/properties/bindings/items/consumer", "/bindings/0/consumer"),
		bindings("bindings#/properties/bindings/items/dependency", "/bindings/0/dependency"),
		bindings("bindings#/properties/bindings/items/source", "/bindings/0/source"),
		bindings("bindings#/properties/bindings/items/capability", "/bindings/0/capability"),
		bindings("bindings#/properties/bindings/items/context", "/bindings/0/context"),
		bindings("bindings#/properties/bindings/items/endpoint", "/bindings/0/endpoint"),
		bindings("bindings#/properties/bindings/items/properties/endpoint/host", "/bindings/0/endpoint/host"),
		bindings("bindings#/properties/bindings/items/properties/endpoint/port", "/bindings/0/endpoint/port"),
		versionInfo("version-info#/apiVersion", "/apiVersion"),
		versionInfo("version-info#/kind", "/kind"),
		versionInfo("version-info#/cli", "/cli"),
		versionInfo("version-info#/model", "/model"),
		versionInfo("version-info#/properties/cli/version", "/cli/version"),
		versionInfo("version-info#/properties/cli/go", "/cli/go"),
		versionInfo("version-info#/properties/cli/platform", "/cli/platform"),
		versionInfo("version-info#/properties/model/version", "/model/version"),
		versionInfo("version-info#/properties/model/apiVersions", "/model/apiVersions"),
		versionInfo("version-info#/properties/engine/version", "/engine/version"),
		versionInfo("version-info#/properties/engine/capabilities", "/engine/capabilities"),
		versionInfo("version-info#/properties/engine/stateVersions", "/engine/stateVersions"),
		versionInfo("version-info#/properties/engine/storeVersions", "/engine/storeVersions"),
		versionInfo("version-info#/properties/engine/properties/stateVersions/read", "/engine/stateVersions/read"),
		versionInfo("version-info#/properties/engine/properties/stateVersions/write", "/engine/stateVersions/write"),
		versionInfo("version-info#/properties/engine/properties/storeVersions/read", "/engine/storeVersions/read"),
		versionInfo("version-info#/properties/engine/properties/storeVersions/write", "/engine/storeVersions/write"),
		manifest("manifest#/apiVersion", "/apiVersion"),
		manifest("manifest#/kind", "/kind"),
		manifest("manifest#/bundleVersion", "/bundleVersion"),
		manifest("manifest#/modules", "/modules"),
		lock("lock#/apiVersion", "/apiVersion"),
		lock("lock#/kind", "/kind"),
		lock("lock#/bundle", "/bundle"),
		lock("lock#/modules", "/modules"),
	}
}

// TestModelRequiredFields_EveryRequiredMemberHasARow walks every schema file for "required" lists
// (prohibitions under "not" and conditions under "if" are not requirements) and fails on any entry
// without a row, so a new required member cannot ship untested.
func TestModelRequiredFields_EveryRequiredMemberHasARow(t *testing.T) {
	t.Parallel()
	rows := map[string]bool{}
	for _, row := range requiredRows() {
		if rows[row.schema] {
			t.Errorf("two rows for %s", row.schema)
		}
		rows[row.schema] = true
	}
	var declared []string
	for _, stem := range publishedSchemaStems(t) {
		if slices.Contains(schemaOnlyStems(), stem) {
			continue
		}
		declared = append(declared, requiredEntries(readPublishedSchema(t, stem), stem+"#")...)
	}
	for _, entry := range declared {
		if !rows[entry] {
			t.Errorf("required member %s has no row", entry)
		}
	}
	for row := range rows {
		if !slices.Contains(declared, row) {
			t.Errorf("row %s names no required member", row)
		}
	}
}

// schemaOnlyStems are the published schemas that no model reader loads. Their required rows live in
// the stem's own <stem>_schema_test.go, checked both ways there; this walk covers the model's
// documents only.
func schemaOnlyStems() []string { return []string{"operation"} }

// TestModelRequiredFields_SchemaOnlyStemsOwnTheirRows fails when a schema-only stem has no test
// file of its own, so skipping it above cannot leave its required members unchecked.
func TestModelRequiredFields_SchemaOnlyStemsOwnTheirRows(t *testing.T) {
	t.Parallel()
	for _, stem := range schemaOnlyStems() {
		if _, err := os.Stat(stem + "_schema_test.go"); err != nil {
			t.Errorf("schema-only stem %s has no %s_schema_test.go: %v", stem, stem, err)
		}
		if !slices.Contains(publishedSchemaStems(t), stem) {
			t.Errorf("schema-only stem %s names no published schema", stem)
		}
	}
}

func requiredEntries(node any, pointer string) []string {
	var entries []string
	switch value := node.(type) {
	case map[string]any:
		if required, ok := value["required"].([]any); ok {
			for _, field := range required {
				if name, isName := field.(string); isName {
					entries = append(entries, pointer+"/"+name)
				}
			}
		}
		for key, child := range value {
			if key == "not" || key == "if" {
				continue
			}
			entries = append(entries, requiredEntries(child, pointer+"/"+key)...)
		}
	case []any:
		for index, child := range value {
			entries = append(entries, requiredEntries(child, pointer+"/"+strconv.Itoa(index))...)
		}
	}
	return entries
}

func TestModelRequiredFields_RemovingOneGivesOneDiagnostic(t *testing.T) {
	t.Parallel()
	for _, row := range requiredRows() {
		t.Run(row.schema, func(t *testing.T) {
			t.Parallel()
			dir := copyCase(t, row.dir)
			removeMember(t, filepath.Join(dir, filepath.FromSlash(row.file)), row.remove)
			if row.subject == subjectBindings {
				checkBindingsSchemaRow(t, dir, row)
				return
			}
			diagnostics := loadRow(t, dir, row)
			if len(diagnostics) != 1 {
				t.Fatalf("want one diagnostic, got %d: %v", len(diagnostics), describeModel(diagnostics))
			}
			got := diagnostics[0]
			wantFile := row.file
			if row.subject == subjectVersionInfo {
				wantFile = ""
			}
			if got.Code() != row.code || got.Location().Pointer() != row.pointer || got.Location().File() != wantFile {
				t.Fatalf("got %s at %s %s, want %s at %s %s", got.Code(), got.Location().File(), got.Location().Pointer(), row.code, wantFile, row.pointer)
			}
		})
	}
}

// copyCase copies a conformance case, or a repository directory given relative to the cases
// directory, into a fresh directory.
func copyCase(t *testing.T, dir string) string {
	t.Helper()
	target := t.TempDir()
	if err := os.CopyFS(target, os.DirFS(filepath.Join(conformanceCases, filepath.FromSlash(dir)))); err != nil {
		t.Fatalf("copy %s: %v", dir, err)
	}
	return target
}

// removeMember deletes the member at pointer from a YAML or JSON file and writes the file back as
// YAML, which every reader accepts.
func removeMember(t *testing.T, path, pointer string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	tokens := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	node := document.Content[0]
	for _, token := range tokens[:len(tokens)-1] {
		node = childNode(t, node, unescapePointer(token))
	}
	last := unescapePointer(tokens[len(tokens)-1])
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Kind == yaml.MappingNode && node.Content[index].Value == last {
			node.Content = slices.Delete(node.Content, index, index+2)
			encoded, err := yaml.Marshal(&document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("%s has no member %s", path, pointer)
}

func childNode(t *testing.T, node *yaml.Node, token string) *yaml.Node {
	t.Helper()
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == token {
				return node.Content[index+1]
			}
		}
	case yaml.SequenceNode:
		if index, err := strconv.Atoi(token); err == nil && index < len(node.Content) {
			return node.Content[index]
		}
	}
	t.Fatalf("no member %q", token)
	return nil
}

func unescapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}

func loadRow(t *testing.T, dir string, row requiredRow) model.Diagnostics {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	switch row.subject {
	case subjectModule:
		_, diagnostics := model.LoadModuleDefinition(root.FS())
		return diagnostics
	case subjectBundle:
		_, diagnostics := model.LoadBundle(root.FS(), row.file)
		return diagnostics
	case subjectLock:
		_, diagnostics := model.LoadLock(root.FS(), row.file)
		return diagnostics
	case subjectVersionInfo:
		data, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatal(err)
		}
		_, diagnostics := model.ParseVersionInfo(data)
		return diagnostics
	default:
		var spec struct {
			Root     string `json:"root"`
			Packaged []struct {
				Repository string `json:"repository"`
				Case       string `json:"case"`
			} `json:"packaged"`
		}
		data, err := os.ReadFile(filepath.Join(dir, "case.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatal(err)
		}
		var packaged []model.ModuleDefinition
		for _, source := range spec.Packaged {
			packagedDir := filepath.Join(dir, filepath.FromSlash(source.Case))
			if source.Repository != "" {
				packagedDir = filepath.Join("../../modules", source.Repository)
			}
			definition, diagnostics := model.LoadModuleDefinition(os.DirFS(packagedDir))
			if len(diagnostics) > 0 {
				t.Fatalf("packaged %s: %v", packagedDir, describeModel(diagnostics))
			}
			packaged = append(packaged, definition)
		}
		_, diagnostics := model.Load(root.FS(), spec.Root, packaged)
		return diagnostics
	}
}

// checkBindingsSchemaRow validates a Bindings document directly: readers only write that document.
func checkBindingsSchemaRow(t *testing.T, dir string, row requiredRow) {
	t.Helper()
	set, err := schemacheck.CompileFS(schemas.Files(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, row.file))
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	result, err := set.Check(schemaBase+"bindings", normalizeYAML(document), requiredFieldCheckLimit)
	if err != nil {
		t.Fatal(err)
	}
	violations := result.Violations
	if result.Truncated || len(violations) != 1 || violations[0].Kind != schemacheck.MissingField || violations[0].Pointer != row.pointer {
		t.Fatalf("want one missing field at %s, got %+v", row.pointer, result)
	}
}

// normalizeYAML turns the YAML library's integers into the JSON numbers the schema checker reads.
func normalizeYAML(value any) any {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			node[key] = normalizeYAML(child)
		}
		return node
	case []any:
		for index, child := range node {
			node[index] = normalizeYAML(child)
		}
		return node
	case int:
		return json.Number(strconv.Itoa(node))
	default:
		return value
	}
}

func describeModel(diagnostics model.Diagnostics) []string {
	var lines []string
	for _, diagnostic := range diagnostics {
		lines = append(lines, string(diagnostic.Code())+" "+diagnostic.Location().File()+" "+diagnostic.Location().Pointer()+": "+diagnostic.Message())
	}
	return lines
}
