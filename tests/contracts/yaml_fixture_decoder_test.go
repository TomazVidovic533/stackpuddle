package contracts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type fixtureDecoderError struct {
	classification string
	path           string
}

func (err *fixtureDecoderError) Error() string {
	return fmt.Sprintf("%s: fixture decoder %s", err.path, err.classification)
}

func TestFixtureYAMLDecoder_RejectsUnsupportedConstructs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		document       string
		classification string
		path           string
	}{
		{name: "multiple documents", document: "apiVersion: stackpuddle/v1\n---\nkind: Manifest\n", classification: "multiple-documents", path: "/"},
		{name: "non-string key", document: "1: value\n", classification: "non-string-key", path: "/"},
		{name: "duplicate key", document: "kind: Manifest\nkind: Lock\n", classification: "duplicate-key", path: "/kind"},
		{name: "alias", document: "value: &value text\ncopy: *value\n", classification: "alias", path: "/copy"},
		{name: "merge key", document: "defaults: &defaults\n  value: text\nobject:\n  <<: *defaults\n", classification: "merge-key", path: "/object/<<"},
		{name: "custom tag", document: "value: !custom text\n", classification: "custom-tag", path: "/value"},
		{name: "NaN", document: "value: .nan\n", classification: "non-json-number", path: "/value"},
		{name: "positive infinity", document: "value: .inf\n", classification: "non-json-number", path: "/value"},
		{name: "negative infinity", document: "value: -.inf\n", classification: "non-json-number", path: "/value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeFixtureYAML([]byte(test.document))

			if err == nil {
				t.Fatal("expected fixture decoding to fail")
			}
			assertFixtureDecoderError(t, err, test.classification, test.path)
		})
	}
}

func TestFixtureYAMLDecoder_ProducesJSONCompatibleValues(t *testing.T) {
	t.Parallel()

	value := readFixtureYAML(t, []byte("enabled: true\ncount: 2\n"))

	if _, err := json.Marshal(value); err != nil {
		t.Fatalf("marshal decoded YAML as JSON: %v", err)
	}
}

func assertFixtureDecoderError(t *testing.T, err error, classification, path string) {
	t.Helper()

	var decoderError *fixtureDecoderError
	if !errors.As(err, &decoderError) {
		t.Fatalf("expected fixture decoder error, got %T: %v", err, err)
	}
	if decoderError.classification != classification || decoderError.path != path {
		t.Fatalf("expected %s at %s, got %s at %s", classification, path, decoderError.classification, decoderError.path)
	}
}

func readFixtureYAML(t *testing.T, contents []byte) any {
	t.Helper()

	value, err := decodeFixtureYAML(contents)
	if err != nil {
		t.Fatalf("decode fixture YAML: %v", err)
	}
	return value
}

func decodeFixtureYAML(contents []byte) (any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))

	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, newFixtureDecoderError("multiple-documents", "")
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, newFixtureDecoderError("multiple-documents", "")
		}
		return nil, err
	}

	return fixtureYAMLNodeValue(document.Content[0], "")
}

func fixtureYAMLNodeValue(node *yaml.Node, path string) (any, error) {
	if node.Alias != nil || node.Kind == yaml.AliasNode {
		return nil, newFixtureDecoderError("alias", path)
	}
	if strings.HasPrefix(node.Tag, "!") && !strings.HasPrefix(node.Tag, "!!") {
		return nil, newFixtureDecoderError("custom-tag", path)
	}

	switch node.Kind {
	case yaml.MappingNode:
		return fixtureYAMLMappingValue(node, path)
	case yaml.SequenceNode:
		return fixtureYAMLSequenceValue(node, path)
	case yaml.ScalarNode:
		return fixtureYAMLScalarValue(node, path)
	default:
		return nil, fmt.Errorf("%s: unsupported YAML node", displayPath(path))
	}
}

func fixtureYAMLMappingValue(node *yaml.Node, path string) (map[string]any, error) {
	result := make(map[string]any, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index]
		if key.Value == "<<" {
			return nil, newFixtureDecoderError("merge-key", joinPath(path, key.Value))
		}
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, newFixtureDecoderError("non-string-key", path)
		}
		if _, exists := result[key.Value]; exists {
			return nil, newFixtureDecoderError("duplicate-key", joinPath(path, key.Value))
		}

		value, err := fixtureYAMLNodeValue(node.Content[index+1], joinPath(path, key.Value))
		if err != nil {
			return nil, err
		}
		result[key.Value] = value
	}
	return result, nil
}

func fixtureYAMLSequenceValue(node *yaml.Node, path string) ([]any, error) {
	result := make([]any, len(node.Content))
	for index, child := range node.Content {
		value, err := fixtureYAMLNodeValue(child, joinPath(path, fmt.Sprintf("%d", index)))
		if err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

func fixtureYAMLScalarValue(node *yaml.Node, path string) (any, error) {
	var result any
	if err := node.Decode(&result); err != nil {
		return nil, fmt.Errorf("%s: decode scalar: %w", displayPath(path), err)
	}
	if number, ok := result.(float64); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
		return nil, newFixtureDecoderError("non-json-number", path)
	}
	return result, nil
}

func newFixtureDecoderError(classification, path string) error {
	return &fixtureDecoderError{classification: classification, path: displayPath(path)}
}
