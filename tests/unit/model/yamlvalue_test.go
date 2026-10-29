package model_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/model/yamlvalue"
)

func defaultLimits() yamlvalue.Limits {
	return yamlvalue.Limits{FileBytes: 1 << 20, Depth: 64, KeyBytes: 1024, Values: 1 << 16, Violations: 100}
}

func TestDecodeClassifiesEachUnsupportedConstruct(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		document string
		want     yamlvalue.Violation
	}{
		"invalid YAML":         {"x: 1\na: b: c\n", yamlvalue.Violation{Class: yamlvalue.InvalidYAML, At: yamlvalue.Position{Line: 2}}},
		"second document":      {"a: 1\n---\nb: 2\n", yamlvalue.Violation{Class: yamlvalue.MultipleDocuments, At: yamlvalue.Position{Line: 2, Column: 1}}},
		"duplicate key":        {"a: 1\na: 2\n", yamlvalue.Violation{Class: yamlvalue.DuplicateKey, Pointer: "/a", At: yamlvalue.Position{Line: 2, Column: 1}, Related: yamlvalue.Position{Line: 1, Column: 1}}},
		"integer key":          {"a:\n  1: x\n", yamlvalue.Violation{Class: yamlvalue.NonStringKey, Pointer: "/a", At: yamlvalue.Position{Line: 2, Column: 3}}},
		"null key":             {"~: x\n", yamlvalue.Violation{Class: yamlvalue.NonStringKey, At: yamlvalue.Position{Line: 1, Column: 1}}},
		"alias":                {"a: &x 1\nb: *x\n", yamlvalue.Violation{Class: yamlvalue.Alias, Pointer: "/b", At: yamlvalue.Position{Line: 2, Column: 4}}},
		"merge key":            {"a:\n  <<: {b: 1}\n", yamlvalue.Violation{Class: yamlvalue.MergeKey, Pointer: "/a/<<", At: yamlvalue.Position{Line: 2, Column: 3}}},
		"custom tag":           {"a: !env HOME\n", yamlvalue.Violation{Class: yamlvalue.CustomTag, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"binary tag":           {"a: !!binary aGk=\n", yamlvalue.Violation{Class: yamlvalue.CustomTag, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"not a number":         {"a: .nan\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"infinity":             {"a: [-.inf]\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a/0", At: yamlvalue.Position{Line: 1, Column: 5}}},
		"hexadecimal":          {"a: 0x1F\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"octal":                {"a: 0o17\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"leading zero":         {"a: 017\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"digit separator":      {"a: 1_000\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"tagged beyond double": {"a: !!float 1e400\n", yamlvalue.Violation{Class: yamlvalue.NonJSONNumber, Pointer: "/a", At: yamlvalue.Position{Line: 1, Column: 4}}},
		"empty value":          {"a:\nb: 1\n", yamlvalue.Violation{Class: yamlvalue.NullValue, Pointer: "/a"}},
		"null":                 {"a: [null]\n", yamlvalue.Violation{Class: yamlvalue.NullValue, Pointer: "/a/0", At: yamlvalue.Position{Line: 1, Column: 5}}},
		"latin-1 byte":         {"a: caf\xe9\n", yamlvalue.Violation{Class: yamlvalue.InvalidEncoding, At: yamlvalue.Position{Line: 1, Column: 7}}},
		"escaped key slash":    {"a/b:\n  ~: 1\n", yamlvalue.Violation{Class: yamlvalue.NonStringKey, Pointer: "/a~1b", At: yamlvalue.Position{Line: 2, Column: 3}}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, _, violations := yamlvalue.Decode([]byte(test.document), defaultLimits())
			if value != nil || len(violations) != 1 {
				t.Fatalf("want one violation and no value, got %v and %#v", violations, value)
			}
			got := violations[0]
			if name == "empty value" {
				// The position of an empty scalar is the parser's to choose; class and pointer are contract.
				got.At = yamlvalue.Position{}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestDecodeKeepsOutOfRangePlainNumbersAsText pins the YAML library's resolution: a plain scalar
// beyond the double range is a string, so it never becomes an infinity a JSON reader cannot hold.
func TestDecodeKeepsOutOfRangePlainNumbersAsText(t *testing.T) {
	t.Parallel()
	value, _, violations := yamlvalue.Decode([]byte("a: 1e400\n"), defaultLimits())
	if len(violations) > 0 || !reflect.DeepEqual(value, map[string]any{"a": "1e400"}) {
		t.Fatalf("got %#v and %+v", value, violations)
	}
}

func TestDecodeEnforcesLimits(t *testing.T) {
	t.Parallel()
	limits := yamlvalue.Limits{FileBytes: 64, Depth: 3, KeyBytes: 1024, Values: 1 << 16, Violations: 100}
	_, _, violations := yamlvalue.Decode([]byte("a: "+strings.Repeat("x", 64)+"\n"), limits)
	if len(violations) != 1 || violations[0].Class != yamlvalue.FileTooLarge || violations[0].Pointer != "" {
		t.Fatalf("an oversized file: got %+v", violations)
	}
	if _, _, violations := yamlvalue.Decode([]byte("a: [[1]]\n"), limits); len(violations) != 0 {
		t.Fatalf("depth 3 is within a limit of 3: %+v", violations)
	}
	_, _, violations = yamlvalue.Decode([]byte("a: [[[1]]]\n"), limits)
	want := yamlvalue.Violation{Class: yamlvalue.TooDeep, Pointer: "/a/0/0", At: yamlvalue.Position{Line: 1, Column: 6}}
	if len(violations) != 1 || violations[0] != want {
		t.Fatalf("depth 4: got %+v, want %+v", violations, want)
	}
}

// TestDecodeEnforcesWorkLimits covers the limits that bound memory below the file size: the key
// length YAML leaves open for explicit keys, the value count, and the report length.
func TestDecodeEnforcesWorkLimits(t *testing.T) {
	t.Parallel()
	limits := yamlvalue.Limits{FileBytes: 1 << 20, Depth: 64, KeyBytes: 8, Values: 5, Violations: 2}
	tests := map[string]struct {
		document string
		want     []yamlvalue.Violation
	}{
		"explicit key past the key limit": {
			"a:\n  ? " + strings.Repeat("k", 9) + "\n  : v\n",
			[]yamlvalue.Violation{{Class: yamlvalue.KeyTooLong, Pointer: "/a", At: yamlvalue.Position{Line: 2, Column: 5}}},
		},
		"members past the value limit": {
			"a: [1, 2]\nb: [3, 4]\n",
			[]yamlvalue.Violation{{Class: yamlvalue.TooManyValues, Pointer: "/b", At: yamlvalue.Position{Line: 2, Column: 4}}},
		},
		"failures past the report limit": {
			"[~, ~, ~]\n",
			[]yamlvalue.Violation{
				{Class: yamlvalue.NullValue, Pointer: "/0", At: yamlvalue.Position{Line: 1, Column: 2}},
				{Class: yamlvalue.NullValue, Pointer: "/1", At: yamlvalue.Position{Line: 1, Column: 5}},
				{Class: yamlvalue.TooManyViolations},
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, _, violations := yamlvalue.Decode([]byte(test.document), limits)
			if value != nil || !reflect.DeepEqual(violations, test.want) {
				t.Fatalf("got %+v and %#v, want %+v", violations, value, test.want)
			}
		})
	}
	if _, _, violations := yamlvalue.Decode([]byte("a: [1, 2]\nb: 3\n"), limits); len(violations) != 0 {
		t.Fatalf("five values are within a limit of five: %+v", violations)
	}
}

func TestDecodeYieldsAJSONTreeWithPositions(t *testing.T) {
	t.Parallel()
	document := "name: shop\ncount: 2\nratio: 1.5e3\nenabled: true\nwhen: 2026-10-29\nitems:\n  - first\n  - \"0x1F\"\n"
	value, positions, violations := yamlvalue.Decode([]byte(document), defaultLimits())
	if len(violations) > 0 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
	want := map[string]any{
		"name": "shop", "count": json.Number("2"), "ratio": json.Number("1.5e3"), "enabled": true,
		"when": "2026-10-29", "items": []any{"first", "0x1F"},
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("got %#v, want %#v", value, want)
	}
	if span, ok := positions.Span("/items/1"); !ok || span.Value != (yamlvalue.Position{Line: 8, Column: 5}) {
		t.Fatalf("position of /items/1 = %+v, %t", span, ok)
	}
	if at := positions.Locate("/items/1/missing"); at != (yamlvalue.Position{Line: 8, Column: 5}) {
		t.Fatalf("an absent member must resolve to its nearest present ancestor, got %+v", at)
	}
}

// TestPositionsReadEscapedPointers covers keys holding "/" and "~", which a pointer escapes, and an
// index written with a leading zero, which names no item.
func TestPositionsReadEscapedPointers(t *testing.T) {
	t.Parallel()
	document := "paths:\n  a/b:\n    x~y: [first, second]\n"
	_, positions, violations := yamlvalue.Decode([]byte(document), defaultLimits())
	if len(violations) > 0 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
	if span, ok := positions.Span("/paths/a~1b/x~0y/1"); !ok || span.Value != (yamlvalue.Position{Line: 3, Column: 18}) {
		t.Fatalf("position of the escaped member = %+v, %t", span, ok)
	}
	if _, ok := positions.Span("/paths/a~1b/x~0y/01"); ok {
		t.Fatal("an index with a leading zero must name no item")
	}
	if at := positions.Locate("/paths/a~1b/x~0y/01"); at != (yamlvalue.Position{Line: 3, Column: 5}) {
		t.Fatalf("an unknown index must resolve to its sequence's key, got %+v", at)
	}
}
