package contracts_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/documents"
)

const operationRunning = "valid/operation-running.json"

func TestOperationSchema_AcceptsEveryValidFixture(t *testing.T) {
	t.Parallel()
	fixtures, err := filepath.Glob(filepath.Join("testdata", "contracts", "v1", "valid", "operation-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 4 {
		t.Fatalf("expected the four reviewed valid operation records, found %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		name := filepath.Base(fixture)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertValidFixture(t, "operation", "valid/"+name)
		})
	}
}

func TestOperationSchema_AcceptsATokenActor(t *testing.T) {
	t.Parallel()
	document := loadContractFixture(t, "valid/operation-token-actor.json")
	if kind := documentAt(t, document, "actor", "kind"); kind != "token" {
		t.Fatalf("the token-actor fixture carries actor kind %v", kind)
	}
	assertValidFixture(t, "operation", "valid/operation-token-actor.json")
}

func TestOperationSchema_RejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	t.Run("unknown field", func(t *testing.T) {
		t.Parallel()
		assertInvalidFixture(t, "operation", "invalid/operation-unknown-field.json", "/x", "additionalProperties")
	})
	t.Run("unknown status", func(t *testing.T) {
		t.Parallel()
		assertInvalidFixture(t, "operation", "invalid/operation-unknown-status.json", "/status", "enum")
	})
	t.Run("unknown actor kind", func(t *testing.T) {
		t.Parallel()
		assertInvalidFixture(t, "operation", "invalid/operation-unknown-actor.json", "/actor/kind", "enum")
	})
	t.Run("job exit code as a string", func(t *testing.T) {
		t.Parallel()
		assertInvalidFixture(t, "operation", "invalid/operation-job-exitcode-string.json", "/steps/0/job/exitCode", "type")
	})
	t.Run("missing id", func(t *testing.T) {
		t.Parallel()
		assertFixtureMissingRequiredField(t, "operation", "invalid/operation-missing-id.json", "", "id")
	})
}

type operationRejection struct {
	name     string
	fixture  string
	path     []any
	value    any
	location string
	keyword  string
}

func TestOperationSchema_EnforcesContractBoundaries(t *testing.T) {
	t.Parallel()

	both := map[string]any{"all": true, "refs": []any{"database/primary"}}
	tests := []operationRejection{
		{name: "lower-case operation id", fixture: operationRunning, path: []any{"id"}, value: "op-01j9z8k3m4n5p6q7r8s9t0v1w2", location: "/id", keyword: "pattern"},
		{name: "continues is not an operation id", fixture: "valid/operation-succeeded-continues.json", path: []any{"continues"}, value: "sc-01J9Z8K3M4N5P6Q7R8S9T0V1W3", location: "/continues", keyword: "pattern"},
		{name: "scope uid with a forbidden letter", fixture: operationRunning, path: []any{"scope", "scopeUid"}, value: "sc-01J9Z8K3M4N5P6Q7R8S9T0V1WU", location: "/scope/scopeUid", keyword: "pattern"},
		{name: "upper-case project", fixture: operationRunning, path: []any{"scope", "project"}, value: "Shop", location: "/scope/project", keyword: "pattern"},
		{name: "unknown command", fixture: operationRunning, path: []any{"command"}, value: "restart", location: "/command", keyword: "enum"},
		{name: "revision zero", fixture: operationRunning, path: []any{"revision"}, value: 0, location: "/revision", keyword: "minimum"},
		{name: "time with an offset", fixture: operationRunning, path: []any{"createdAt"}, value: "2026-10-29T10:00:00+02:00", location: "/createdAt", keyword: "pattern"},
		{name: "time that is not a date", fixture: operationRunning, path: []any{"updatedAt"}, value: "2026-13-45T10:00:00Z", location: "/updatedAt", keyword: "format"},
		{name: "selection with both all and refs", fixture: operationRunning, path: []any{"selection"}, value: both, location: "/selection", keyword: "oneOf"},
		{name: "selection with no refs", fixture: "valid/operation-succeeded-continues.json", path: []any{"selection", "refs"}, value: []any{}, location: "/selection/refs", keyword: "minItems"},
		{name: "selection with a repeated ref", fixture: "valid/operation-succeeded-continues.json", path: []any{"selection", "refs"}, value: []any{"database/primary", "database/primary"}, location: "/selection/refs", keyword: "uniqueItems"},
		{name: "step action without a namespace", fixture: operationRunning, path: []any{"steps", 0, "action"}, value: "create", location: "/steps/0/action", keyword: "pattern"},
		{name: "step id without a number", fixture: operationRunning, path: []any{"steps", 0, "id"}, value: "step", location: "/steps/0/id", keyword: "pattern"},
		{name: "unknown step status", fixture: operationRunning, path: []any{"steps", 0, "status"}, value: "running", location: "/steps/0/status", keyword: "enum"},
		{name: "unknown step field", fixture: operationRunning, path: []any{"steps", 0, "evidence"}, value: "text", location: "/steps/0/evidence", keyword: "additionalProperties"},
		{name: "error with a message", fixture: "valid/operation-blocked-error.json", path: []any{"error", "message"}, value: "volume is gone", location: "/error/message", keyword: "additionalProperties"},
		{name: "error with an unknown category", fixture: "valid/operation-blocked-error.json", path: []any{"error", "category"}, value: "bad", location: "/error/category", keyword: "enum"},
		{name: "actor with a user name", fixture: operationRunning, path: []any{"actor", "name"}, value: "alice", location: "/actor/name", keyword: "additionalProperties"},
		{name: "wrong kind", fixture: operationRunning, path: []any{"kind"}, value: "Lock", location: "/kind", keyword: "const"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := loadContractFixture(t, test.fixture)
			setAt(t, document, test.path, test.value)

			err := validateContract(t, "operation", document)

			if err == nil {
				t.Fatal("expected contract validation to fail")
			}
			assertValidationFailure(t, err, test.location, test.keyword)
		})
	}
}

type operationRequiredRow struct {
	fixture string
	path    []any
}

// operationRequiredRows maps every required member of operation.schema.json, by "operation#" plus
// its schema pointer, to a valid fixture and the path to remove.
func operationRequiredRows() map[string]operationRequiredRow {
	blocked := "valid/operation-blocked-error.json"
	continues := "valid/operation-succeeded-continues.json"
	return map[string]operationRequiredRow{
		"operation#/apiVersion":                         {operationRunning, []any{"apiVersion"}},
		"operation#/kind":                               {operationRunning, []any{"kind"}},
		"operation#/id":                                 {operationRunning, []any{"id"}},
		"operation#/scope":                              {operationRunning, []any{"scope"}},
		"operation#/command":                            {operationRunning, []any{"command"}},
		"operation#/selection":                          {operationRunning, []any{"selection"}},
		"operation#/revision":                           {operationRunning, []any{"revision"}},
		"operation#/status":                             {operationRunning, []any{"status"}},
		"operation#/steps":                              {operationRunning, []any{"steps"}},
		"operation#/actor":                              {operationRunning, []any{"actor"}},
		"operation#/createdAt":                          {operationRunning, []any{"createdAt"}},
		"operation#/updatedAt":                          {operationRunning, []any{"updatedAt"}},
		"operation#/properties/scope/project":           {operationRunning, []any{"scope", "project"}},
		"operation#/properties/scope/environment":       {operationRunning, []any{"scope", "environment"}},
		"operation#/properties/scope/scopeUid":          {operationRunning, []any{"scope", "scopeUid"}},
		"operation#/properties/actor/kind":              {operationRunning, []any{"actor", "kind"}},
		"operation#/$defs/problem/code":                 {blocked, []any{"error", "code"}},
		"operation#/$defs/problem/category":             {blocked, []any{"error", "category"}},
		"operation#/$defs/step/id":                      {operationRunning, []any{"steps", 0, "id"}},
		"operation#/$defs/step/action":                  {operationRunning, []any{"steps", 0, "action"}},
		"operation#/$defs/step/object":                  {operationRunning, []any{"steps", 0, "object"}},
		"operation#/$defs/step/status":                  {operationRunning, []any{"steps", 0, "status"}},
		"operation#/$defs/step/properties/job/exitCode": {"valid/operation-run-job-result.json", []any{"steps", 0, "job", "exitCode"}},
		"operation#/$defs/selection/oneOf/0/all":        {operationRunning, []any{"selection", "all"}},
		"operation#/$defs/selection/oneOf/1/refs":       {continues, []any{"selection", "refs"}},
	}
}

// TestOperationSchema_RequiresEveryField removes each required member in turn and expects the
// required failure, and checks the row list against the schema in both directions, so neither a new
// required member nor a stale row can pass (the walk in model_required_fields_test.go skips this
// schema-only stem).
func TestOperationSchema_RequiresEveryField(t *testing.T) {
	t.Parallel()
	rows := operationRequiredRows()

	declared := requiredEntries(readPublishedSchema(t, "operation"), "operation#")
	for _, entry := range declared {
		if _, ok := rows[entry]; !ok {
			t.Errorf("required member %s has no row", entry)
		}
	}
	for entry := range rows {
		if !slices.Contains(declared, entry) {
			t.Errorf("row %s names no required member", entry)
		}
	}

	for entry, row := range rows {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			document := loadContractFixture(t, row.fixture)
			removeAt(t, document, row.path)

			err := validateContract(t, "operation", document)

			if err == nil {
				t.Fatal("expected missing required field to fail validation")
			}
			parent := row.path[:len(row.path)-1]
			assertRequiredFailure(t, err, pointerOf(parent), fmt.Sprint(row.path[len(row.path)-1]))
		})
	}
}

// TestOperationSchema_EveryPropertyAppearsInAValidFixture keeps the schema and its fixtures honest:
// a property no valid record uses has never been shown to validate.
func TestOperationSchema_EveryPropertyAppearsInAValidFixture(t *testing.T) {
	t.Parallel()
	fixtures, err := filepath.Glob(filepath.Join("testdata", "contracts", "v1", "valid", "operation-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, fixture := range fixtures {
		collectKeys(loadContractFixture(t, "valid/"+filepath.Base(fixture)), used)
	}
	for _, property := range schemaProperties(readPublishedSchema(t, "operation")) {
		if !used[property] {
			t.Errorf("property %q appears in no valid operation fixture", property)
		}
	}
}

func collectKeys(node any, keys map[string]bool) {
	switch value := node.(type) {
	case contractDocument:
		collectKeys(map[string]any(value), keys)
	case map[string]any:
		for name, child := range value {
			keys[name] = true
			collectKeys(child, keys)
		}
	case []any:
		for _, child := range value {
			collectKeys(child, keys)
		}
	}
}

func documentAt(t *testing.T, document contractDocument, path ...any) any {
	t.Helper()
	return documents.NodeAt(t, map[string]any(document), path...)
}

func setAt(t *testing.T, document contractDocument, path []any, value any) {
	t.Helper()
	parent := documentAt(t, document, path[:len(path)-1]...)
	switch container := parent.(type) {
	case map[string]any:
		key, ok := path[len(path)-1].(string)
		if !ok {
			t.Fatalf("expected a member name, got %T", path[len(path)-1])
		}
		container[key] = value
	case []any:
		index, ok := path[len(path)-1].(int)
		if !ok {
			t.Fatalf("expected a list index, got %T", path[len(path)-1])
		}
		container[index] = value
	default:
		t.Fatalf("cannot set inside %T", parent)
	}
}

func removeAt(t *testing.T, document contractDocument, path []any) {
	t.Helper()
	parent, ok := documentAt(t, document, path[:len(path)-1]...).(map[string]any)
	if !ok {
		t.Fatalf("expected an object holding %v", path)
	}
	key, ok := path[len(path)-1].(string)
	if !ok {
		t.Fatalf("expected a member name, got %T", path[len(path)-1])
	}
	delete(parent, key)
}

func pointerOf(path []any) string {
	tokens := make([]string, len(path))
	for index, segment := range path {
		switch key := segment.(type) {
		case string:
			tokens[index] = key
		case int:
			tokens[index] = strconv.Itoa(key)
		}
	}
	return jsonPointer(tokens)
}
