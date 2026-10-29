// Package allocation_test measures what Load costs on hostile input, in heap growth, files read and
// time, alone: no test in this package runs in parallel, so nothing skews the runtime's counters.
package allocation_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const (
	rootFile = "stackpuddle.yaml"
	// fileLimit is the parser's per-file size limit; each crafted file fills it.
	fileLimit = 1 << 20
	// reportLimit is the parser's per-file diagnostic limit.
	reportLimit = 100
	// nestedKeys and nestedKeyBytes stay inside the depth and key-length limits, so only a pointer
	// copied for every node below them, which once cost gigabytes, could make the file expensive.
	nestedKeys     = 60
	nestedKeyBytes = 1000
	// allocationCeiling bounds what Load may allocate for one crafted file. The YAML parser alone
	// allocates about 120 bytes per input byte for the densest input; the ceiling sits above that
	// and far below the cost of anything that grows with nodes times path length.
	allocationCeiling = 192 << 20
	// faultsBelowValueLimit is far above the report limit and below the per-file value limit.
	faultsBelowValueLimit = 50_000
	projectHeader         = "apiVersion: stackpuddle/v1\nkind: Project\nmetadata: {name: shop}\n"
	environment           = "  environments: {local: {runtime: {type: compose, context: default}, secrets: {provider: local}}}\n"

	// sharingDefinitions entries name one definition directory. Each takes its own name, so each is
	// reported as not matching the name in module.yaml.
	sharingDefinitions = 4096
	// sharedWorkloads, sharedVariables and sharedValueBytes are the schema's maxima of workloads and
	// environment variables, with values long enough to bring module.yaml near the file limit.
	sharedWorkloads  = 16
	sharedVariables  = 128
	sharedValueBytes = 480
)

func TestLoadBoundsAllocationForHostileFiles(t *testing.T) {
	tests := map[string]struct {
		files fstest.MapFS
		// want is the diagnostic count by code.
		want map[model.Code]int
	}{
		"nested long keys over a long flow sequence": {
			files: rootOnly(nestedFlowSequence()),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
		"an explicit key longer than the key limit": {
			files: rootOnly("? " + strings.Repeat("k", fileLimit-16) + "\n: v\n"),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
		"a null in every item": {
			files: rootOnly("items: [" + strings.Repeat("~, ", faultsBelowValueLimit) + "~]\n"),
			want:  map[model.Code]int{model.SyntaxNullValue: reportLimit, model.SyntaxLimitExceeded: 1},
		},
		"an unknown field on every line": {
			files: rootOnly(unknownFields()),
			want:  map[model.Code]int{model.StructureUnknownField: reportLimit, model.SyntaxLimitExceeded: 1},
		},
		"deeply nested options against an equally deep option schema": {
			files: deepOptions(),
			want:  map[model.Code]int{model.StructureInvalidType: reportLimit, model.SyntaxLimitExceeded: 1},
		},
		"thousands of definitions naming one module.yaml near the file limit": {
			files: sharedDefinition(),
			want:  map[model.Code]int{model.IdentityDefinitionNameMismatch: sharingDefinitions},
		},
		"an option schema with a subschema for every value the file limit allows": {
			files: wideOptionSchema(),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
		"chained anyOf references 30 levels deep": {
			files: chainedOptionSchema(30),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
		"chained anyOf references 40 levels deep": {
			files: chainedOptionSchema(40),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
		"a draft-07 tuple items chain 40 levels deep": {
			files: tupleChain("http://json-schema.org/draft-07/schema#", 40),
			want:  map[model.Code]int{model.StructureInvalidSchema: 1},
		},
		"a 2019-09 tuple items chain 40 levels deep": {
			files: tupleChain("https://json-schema.org/draft/2019-09/schema", 40),
			want:  map[model.Code]int{model.StructureInvalidSchema: 1},
		},
		"a percent-encoded reference chain 40 levels deep": {
			files: encodedChain(40),
			want:  map[model.Code]int{model.StructureInvalidSchema: 1},
		},
		"an option schema that references itself": {
			files: optionSchemaFiles(`{"type": "object", "additionalProperties": {"$ref": "#"}}`, "{a: 1}"),
			want:  map[model.Code]int{model.StructureInvalidSchema: 1},
		},
		"many failing branches against many options": {
			files: failingBranches(),
			want:  map[model.Code]int{model.SyntaxLimitExceeded: 1},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, diagnostics := model.Load(test.files, rootFile, nil)
			runtime.ReadMemStats(&after)
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("%d diagnostics; Load allocated %d bytes (%.1f MiB)", len(diagnostics), allocated, float64(allocated)/(1<<20))
			if got := countByCode(diagnostics); !equalCounts(got, test.want) {
				t.Fatalf("diagnostics by code: got %v, want %v", got, test.want)
			}
			if allocated >= allocationCeiling {
				t.Fatalf("Load allocated %d bytes, want below %d", allocated, allocationCeiling)
			}
		})
	}
}

func rootOnly(data string) fstest.MapFS { return fstest.MapFS{rootFile: {Data: []byte(data)}} }

// nestedFlowSequence nests mappings under long keys and fills the rest of the file with one flow
// sequence of plain scalars, each of which sits below every key.
func nestedFlowSequence() string {
	var builder strings.Builder
	for range nestedKeys {
		builder.WriteString("{" + strings.Repeat("k", nestedKeyBytes) + ": ")
	}
	builder.WriteString("[")
	for builder.Len() < fileLimit-2*nestedKeys {
		builder.WriteString("b,")
	}
	builder.WriteString("b]" + strings.Repeat("}", nestedKeys) + "\n")
	return builder.String()
}

func unknownFields() string {
	var builder strings.Builder
	builder.WriteString(projectHeader + "spec: {}\n")
	for index := range faultsBelowValueLimit {
		fmt.Fprintf(&builder, "x%d: 1\n", index)
	}
	return builder.String()
}

// deepOptions places many wrongly typed leaves below long nested keys inside one module instance's
// options, against a local option schema as deep as the options, so the validator fails every leaf at
// the full path length.
func deepOptions() fstest.MapFS {
	const optionKeys = 50
	var builder strings.Builder
	builder.WriteString(projectHeader + "spec:\n  definitions: {deep: {path: deep}}\n  modules:\n    store: {definition: deep, options: ")
	for range optionKeys {
		builder.WriteString("{" + strings.Repeat("k", nestedKeyBytes) + ": ")
	}
	builder.WriteString("{")
	for index := range faultsBelowValueLimit / 4 {
		fmt.Fprintf(&builder, "c%d: 1, ", index)
	}
	builder.WriteString("z: 1}" + strings.Repeat("}", optionKeys) + "}\n" + environment)
	definition := "apiVersion: stackpuddle/v1\nkind: Module\nname: deep\nversion: 0.1.0\n" +
		"configuration:\n  schema: content/options.schema.json\nworkloads:\n  store: {type: service, image: busybox}\n"
	// One nested additionalProperties per key level: the schema may not recurse, so it is as deep as
	// the options. The innermost level fails every leaf.
	schema := `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": `
	for range optionKeys - 1 {
		schema += `{"type": "object", "additionalProperties": `
	}
	schema += `{"type": "object", "additionalProperties": {"type": "object"}}` + strings.Repeat("}", optionKeys-1) + "}"
	return fstest.MapFS{
		rootFile:                           {Data: []byte(builder.String())},
		"deep/module.yaml":                 {Data: []byte(definition)},
		"deep/content/options.schema.json": {Data: []byte(schema)},
	}
}

// sharedDefinition names one module.yaml of nearly the file limit from thousands of definitions, so
// a Load that read the directory once per entry would read gigabytes.
func sharedDefinition() fstest.MapFS {
	var root strings.Builder
	root.WriteString(projectHeader + "spec:\n  definitions:\n")
	for index := range sharingDefinitions {
		fmt.Fprintf(&root, "    d%d: {path: shared}\n", index)
	}
	root.WriteString(environment)
	var definition strings.Builder
	definition.WriteString("apiVersion: stackpuddle/v1\nkind: Module\nname: shared\nversion: 0.1.0\nworkloads:\n")
	value := strings.Repeat("v", sharedValueBytes)
	for workload := range sharedWorkloads {
		fmt.Fprintf(&definition, "  w%d:\n    type: service\n    image: busybox\n    environment:\n", workload)
		for variable := range sharedVariables {
			fmt.Fprintf(&definition, "      E%d: %s\n", variable, value)
		}
	}
	return fstest.MapFS{
		rootFile:             {Data: []byte(root.String())},
		"shared/module.yaml": {Data: []byte(definition.String())},
	}
}

// wideOptionSchema gives a local definition an option schema with one subschema per property, as
// many as the per-file value limit allows. Compiling it costs more than linear time in its
// subschemas, so only the option-schema value limit keeps the Load short.
func wideOptionSchema() fstest.MapFS {
	var schema strings.Builder
	schema.WriteString(`{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "properties": {`)
	for index := range faultsBelowValueLimit {
		fmt.Fprintf(&schema, `"p%d": true, `, index)
	}
	schema.WriteString(`"z": true}}`)
	definition := "apiVersion: stackpuddle/v1\nkind: Module\nname: wide\nversion: 0.1.0\n" +
		"configuration:\n  schema: content/options.schema.json\nworkloads:\n  store: {type: service, image: busybox}\n"
	return fstest.MapFS{
		rootFile:                           {Data: []byte(projectHeader + "spec:\n  definitions: {wide: {path: wide}}\n" + environment)},
		"wide/module.yaml":                 {Data: []byte(definition)},
		"wide/content/options.schema.json": {Data: []byte(schema.String())},
	}
}

// chainedOptionSchema is `levels` definitions, each an anyOf of two references to the next. Written
// out it is a few hundred values; validating it would take 2^levels steps.
func chainedOptionSchema(levels int) fstest.MapFS {
	var schema strings.Builder
	schema.WriteString(`{"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "#/$defs/d0", "$defs": {`)
	for level := range levels {
		fmt.Fprintf(&schema, `"d%d": {"anyOf": [{"$ref": "#/$defs/d%d"}, {"$ref": "#/$defs/d%d"}]}, `, level, level+1, level+1)
	}
	fmt.Fprintf(&schema, `"d%d": {"type": "integer"}}}`, levels)
	return optionSchemaFiles(schema.String(), "{a: 1}")
}

// tupleChain enters the chained definitions through a list-valued items under the draft named by uri.
func tupleChain(uri string, levels int) fstest.MapFS {
	var schema strings.Builder
	fmt.Fprintf(&schema, `{"$schema": %q, "items": [{"$ref": "#/$defs/d0"}], "$defs": {`, uri)
	for level := range levels {
		fmt.Fprintf(&schema, `"d%d": {"anyOf": [{"$ref": "#/$defs/d%d"}, {"$ref": "#/$defs/d%d"}]}, `, level, level+1, level+1)
	}
	fmt.Fprintf(&schema, `"d%d": {"type": "integer"}}}`, levels)
	return optionSchemaFiles(schema.String(), "{a: 1}")
}

// encodedChain names every definition through a percent-encoded reference ("%64" is "d") next to a
// decoy of the literal name, which a validator never reaches.
func encodedChain(levels int) fstest.MapFS {
	var schema strings.Builder
	schema.WriteString(`{"$schema": "https://json-schema.org/draft/2020-12/schema", "$ref": "#/$defs/%640", "$defs": {`)
	for level := range levels {
		fmt.Fprintf(&schema, `"d%d": {"anyOf": [{"$ref": "#/$defs/%%64%d"}, {"$ref": "#/$defs/%%64%d"}]}, "%%64%d": {}, `, level, level+1, level+1, level+1)
	}
	fmt.Fprintf(&schema, `"d%d": {"type": "integer"}}}`, levels)
	return optionSchemaFiles(schema.String(), "{a: 1}")
}

// failingBranches checks failingOptions options against a schema whose additionalProperties is an
// anyOf of failingBranches const branches that every option fails: each fits the schema value limit
// and the file limits, but the work is their product.
func failingBranches() fstest.MapFS {
	const failingBranches, failingOptions = 600, 20_000
	var schema, options strings.Builder
	schema.WriteString(`{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": {"anyOf": [`)
	for branch := range failingBranches {
		fmt.Fprintf(&schema, `{"const": "x%d"}, `, branch)
	}
	schema.WriteString(`{"const": "last"}]}}`)
	options.WriteString("{")
	for option := range failingOptions {
		fmt.Fprintf(&options, "o%d: 1, ", option)
	}
	options.WriteString("z: 1}")
	return optionSchemaFiles(schema.String(), options.String())
}

// optionSchemaFiles is a project with one module instance of a local definition whose option schema
// is schema and whose options are the flow mapping options.
func optionSchemaFiles(schema, options string) fstest.MapFS {
	definition := "apiVersion: stackpuddle/v1\nkind: Module\nname: wide\nversion: 0.1.0\n" +
		"configuration:\n  schema: content/options.schema.json\nworkloads:\n  store: {type: service, image: busybox}\n"
	root := projectHeader + "spec:\n  definitions: {wide: {path: wide}}\n  modules:\n    store: {definition: wide, options: " + options + "}\n" + environment
	return fstest.MapFS{
		rootFile:                           {Data: []byte(root)},
		"wide/module.yaml":                 {Data: []byte(definition)},
		"wide/content/options.schema.json": {Data: []byte(schema)},
	}
}

func countByCode(diagnostics model.Diagnostics) map[model.Code]int {
	counts := map[model.Code]int{}
	for _, diagnostic := range diagnostics {
		counts[diagnostic.Code()]++
	}
	return counts
}

func equalCounts(got, want map[model.Code]int) bool {
	if len(got) != len(want) {
		return false
	}
	for code, count := range want {
		if got[code] != count {
			return false
		}
	}
	return true
}
