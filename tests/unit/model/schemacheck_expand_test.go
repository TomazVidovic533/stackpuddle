package model_test

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
)

const expandLimit = 2048

// chainedSchema is `levels` definitions, each an anyOf of two references to the next; written out it
// is a few hundred values, expanded it holds 2^levels.
func chainedSchema(levels int) string {
	var defs []string
	for level := range levels {
		defs = append(defs, fmt.Sprintf(`"d%d": {"anyOf": [{"$ref": "#/$defs/d%d"}, {"$ref": "#/$defs/d%d"}]}`, level, level+1, level+1))
	}
	defs = append(defs, fmt.Sprintf(`"d%d": {"type": "integer"}`, levels))
	return `{"$defs": {` + strings.Join(defs, ",") + `}, "$ref": "#/$defs/d0"}`
}

// TestExpandedValuesRefusesChainedReferencesFast is the PF2 failure path for the exponential schema:
// 30 and 40 levels are refused with ErrTooLarge, within a time and allocation bound, because each
// definition is sized once.
func TestExpandedValuesRefusesChainedReferencesFast(t *testing.T) {
	for _, levels := range []int{30, 40} {
		t.Run(fmt.Sprint(levels), func(t *testing.T) {
			tree := decodeTree(t, chainedSchema(levels))
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			_, err := schemacheck.ExpandedValues(tree, expandLimit)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, schemacheck.ErrTooLarge) {
				t.Fatalf("got %v, want ErrTooLarge", err)
			}
			if elapsed > time.Second || after.TotalAlloc-before.TotalAlloc > 4<<20 {
				t.Fatalf("took %v and allocated %d bytes, want under 1s and 4 MiB", elapsed, after.TotalAlloc-before.TotalAlloc)
			}
		})
	}
}

func TestExpandedValuesCountsReferencesPerUse(t *testing.T) {
	t.Parallel()
	tree := decodeTree(t, `{"$defs": {"n": {"type": "integer"}}, "properties": {"a": {"$ref": "#/$defs/n"}, "b": {"$ref": "#/$defs/n"}}}`)
	got, err := schemacheck.ExpandedValues(tree, expandLimit)
	if err != nil {
		t.Fatal(err)
	}
	// root, the properties object, and per property its schema object, its "$ref" value and the
	// definition's two values.
	if want := 1 + 1 + 2*(1+1+2); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestExpandedValuesRefusesUnsupportedReferences(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		schema string
		want   error
	}{
		"a cycle":                {`{"$defs": {"a": {"properties": {"x": {"$ref": "#/$defs/a"}}}}, "$ref": "#/$defs/a"}`, schemacheck.ErrReferenceCycle},
		"two definitions cycle":  {`{"$defs": {"a": {"$ref": "#/$defs/b"}, "b": {"$ref": "#/$defs/a"}}, "$ref": "#/$defs/a"}`, schemacheck.ErrReferenceCycle},
		"the root":               {`{"additionalProperties": {"$ref": "#"}}`, schemacheck.ErrUnsupportedReference},
		"a pointer into the doc": {`{"properties": {"a": {}}, "$ref": "#/properties/a"}`, schemacheck.ErrUnsupportedReference},
		"a missing definition":   {`{"$ref": "#/$defs/none"}`, schemacheck.ErrUnsupportedReference},
		"a file":                 {`{"$ref": "other.json"}`, schemacheck.ErrUnsupportedReference},
		"$dynamicRef":            {`{"$dynamicRef": "#x"}`, schemacheck.ErrUnsupportedReference},
		"$anchor":                {`{"properties": {"a": {"$anchor": "x"}}}`, schemacheck.ErrUnsupportedReference},
		"a nested $id":           {`{"properties": {"a": {"$id": "urn:other", "$ref": "#/$defs/x"}}}`, schemacheck.ErrUnsupportedReference},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := schemacheck.ExpandedValues(decodeTree(t, test.schema), expandLimit); !errors.Is(err, test.want) {
				t.Errorf("got %v, want %v", err, test.want)
			}
		})
	}
}

// TestExpandedValuesAcceptsAPropertyNamedLikeAKeyword keeps the walk schema-aware: a property called
// "$ref" or "const" is a name, not a keyword.
func TestExpandedValuesAcceptsAPropertyNamedLikeAKeyword(t *testing.T) {
	t.Parallel()
	tree := decodeTree(t, `{"properties": {"$ref": {"type": "string"}, "$anchor": {"type": "string"}}, "const": {"$ref": "other.json"}}`)
	if _, err := schemacheck.ExpandedValues(tree, expandLimit); err != nil {
		t.Errorf("got %v, want the schema accepted", err)
	}
}

func TestExpandedValuesAcceptsThePostgresOptionSchema(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../modules/postgres/content/configuration/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemacheck.ExpandedValues(decodeTree(t, string(data)), expandLimit); err != nil {
		t.Errorf("got %v, want the module's own schema accepted", err)
	}
}

// bypassChain is a chain of anyOf definitions a validator expands twice per level, entered through
// a form the count must not miss: entry is the tail of a root keyword, spliced into the JSON text.
func bypassChain(levels int, schemaURI, entry string) string {
	var defs []string
	for level := range levels {
		defs = append(defs, fmt.Sprintf(`"d%d": {"anyOf": [{"$ref": "#/$defs/%s"}, {"$ref": "#/$defs/%s"}]}`, level, name(level+1), name(level+1)))
	}
	defs = append(defs, fmt.Sprintf(`"d%d": {"type": "integer"}`, levels))
	return fmt.Sprintf(`{"$schema": %q, "$defs": {%s}, "%s"}`, schemaURI, strings.Join(defs, ","), entry)
}

func name(level int) string { return fmt.Sprintf("d%d", level) }

// TestExpandedValuesRefusesDraftAndEncodingBypasses is the PF2 failure path for schemas whose
// meaning the count could miss: another draft's tuple-form items, and percent-encoded names.
func TestExpandedValuesRefusesDraftAndEncodingBypasses(t *testing.T) {
	const draft07 = "http://json-schema.org/draft-07/schema#"
	const draft201909 = "https://json-schema.org/draft/2019-09/schema"
	const current = schemacheck.Dialect
	tests := map[string]string{
		"draft-07 tuple items":     bypassChain(40, draft07, `items": [{"$ref": "#/$defs/d0"}], "x": "`),
		"2019-09 tuple items":      bypassChain(40, draft201909, `items": [{"$ref": "#/$defs/d0"}], "x": "`),
		"2020-12 tuple items":      bypassChain(40, current, `items": [{"$ref": "#/$defs/d0"}], "x": "`),
		"2020-12 tuple additional": bypassChain(40, current, `additionalItems": [{"$ref": "#/$defs/d0"}], "x": "`),
		"a percent-encoded name":   bypassChain(40, current, `$ref": "#/$defs/%64`+`0`),
	}
	for label, schema := range tests {
		t.Run(label, func(t *testing.T) {
			start := time.Now()
			_, err := schemacheck.ExpandedValues(decodeTree(t, schema), expandLimit)
			if !errors.Is(err, schemacheck.ErrUnsupportedReference) && !errors.Is(err, schemacheck.ErrTooLarge) {
				t.Fatalf("got %v, want the schema refused", err)
			}
			if time.Since(start) > time.Second {
				t.Fatalf("took %v, want under 1s", time.Since(start))
			}
		})
	}
}
