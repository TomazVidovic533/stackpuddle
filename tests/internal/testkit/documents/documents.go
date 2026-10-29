// Package documents decodes and walks JSON documents for tests. It lives under tests/internal,
// so no module that requires stackpuddle can import it.
package documents

import (
	"encoding/json"
	"strings"
	"testing"
)

// DecodeJSON decodes data as one JSON object and fails the test otherwise.
func DecodeJSON(t testing.TB, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode JSON document: %v", err)
	}
	return document
}

// ObjectAt returns the object at path in document and fails the test, naming the first missing
// or non-object segment, when there is none.
func ObjectAt(t testing.TB, document map[string]any, path []string) map[string]any {
	t.Helper()
	object := document
	for index, segment := range path {
		child, exists := object[segment]
		if !exists {
			t.Fatalf("field %s does not exist", strings.Join(path[:index+1], "/"))
		}
		var ok bool
		object, ok = child.(map[string]any)
		if !ok {
			t.Fatalf("expected object at %s, got %T", strings.Join(path[:index+1], "/"), child)
		}
	}
	return object
}

// NodeAt returns the value at path in document, where a string segment names an object member and an
// int segment indexes a list, and fails the test, naming the first segment that does not fit.
func NodeAt(t testing.TB, document map[string]any, path ...any) any {
	t.Helper()
	var node any = document
	for index, segment := range path {
		switch key := segment.(type) {
		case string:
			object, ok := node.(map[string]any)
			if !ok {
				t.Fatalf("expected an object before %q at segment %d, got %T", key, index, node)
			}
			child, exists := object[key]
			if !exists {
				t.Fatalf("field %q does not exist", key)
			}
			node = child
		case int:
			list, ok := node.([]any)
			if !ok || key < 0 || key >= len(list) {
				t.Fatalf("expected a list with index %d at segment %d, got %T", key, index, node)
			}
			node = list[key]
		default:
			t.Fatalf("unsupported path segment %T", segment)
		}
	}
	return node
}
