package packaged_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// provenanceKeys are the keys of an origin record. Only an object that carries "origin" is a record, so a
// declared key that happens to be called file or line (an environment file reference) is never removed.
var provenanceKeys = []string{"origin", "file", "line", "column"}

// withoutProvenance returns doc with every origin record's position and origin removed, leaving what the
// declarations mean. The SDK and the YAML loader describe the same model from different sources, so their
// origins differ by design.
func withoutProvenance(doc any) any {
	switch v := doc.(type) {
	case map[string]any:
		_, isRecord := v["origin"].(string)
		out := make(map[string]any, len(v))
		for key, child := range v {
			if isRecord && slices.Contains(provenanceKeys, key) {
				continue
			}
			out[key] = withoutProvenance(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = withoutProvenance(child)
		}
		return out
	default:
		return doc
	}
}

func decodeAny(t *testing.T, text string) any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return doc
}

func TestProvenance_OnlyOriginRecordsLoseTheirPositionAndOrigin(t *testing.T) {
	sdk := `{"spec":{"env":{"file":"a.env","key":"K"}},"provenance":{"workload/api":{"declared":{"origin":"sdk"}}},"patch":[{"origin":"sdk","fields":["image"]}]}`
	yaml := `{"spec":{"env":{"file":"a.env","key":"K"}},"provenance":{"workload/api":{"declared":{"origin":"yaml","file":"stackpuddle.yaml","line":9,"column":5}}},"patch":[{"origin":"yaml","fields":["image"],"line":3}]}`
	if !reflect.DeepEqual(withoutProvenance(decodeAny(t, sdk)), withoutProvenance(decodeAny(t, yaml))) {
		t.Fatal("documents that differ only in origin records compare unequal")
	}
	changedMeaning := `{"spec":{"env":{"file":"b.env","key":"K"}},"provenance":{"workload/api":{"declared":{"origin":"sdk"}}},"patch":[{"origin":"sdk","fields":["image"]}]}`
	if reflect.DeepEqual(withoutProvenance(decodeAny(t, sdk)), withoutProvenance(decodeAny(t, changedMeaning))) {
		t.Fatal("a changed environment file reference was stripped as provenance")
	}
	otherFields := `{"spec":{"env":{"file":"a.env","key":"K"}},"provenance":{"workload/api":{"declared":{"origin":"sdk"}}},"patch":[{"origin":"sdk","fields":["command"]}]}`
	if reflect.DeepEqual(withoutProvenance(decodeAny(t, sdk)), withoutProvenance(decodeAny(t, otherFields))) {
		t.Fatal("a changed patch field list was stripped as provenance")
	}
}
