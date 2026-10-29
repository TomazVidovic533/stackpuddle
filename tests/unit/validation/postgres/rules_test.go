package postgres_test

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/validation/postgres"
)

// baseOptions is a valid instance: two databases with their own owners, runtime users sharing roles,
// settings only for users with access. Every case below changes it and states what that breaks.
const baseOptions = `{
  "users": {
    "app_runtime": {"purpose": "runtime", "passwordSecretRef": "app-runtime-password", "memberOf": ["app_writer"]},
    "app_migrator": {"purpose": "migrations", "passwordSecretRef": "app-migration-password"},
    "reporting_app": {"purpose": "runtime", "passwordSecretRef": "reporting-app-password", "memberOf": ["reporting_reader"]},
    "reporting_migrator": {"purpose": "migrations", "passwordSecretRef": "reporting-migration-password"}
  },
  "roles": {"app_writer": {"login": false}, "reporting_reader": {"login": false}},
  "databases": {
    "application": {
      "owner": "app_migrator",
      "connectRoles": ["app_writer"],
      "schemas": {"app": {"grants": {"app_writer": {"tables": ["SELECT"]}}}},
      "userSettings": {"app_runtime": {"statementTimeoutMs": 1}, "app_migrator": {"lockTimeoutMs": 0}}
    },
    "reporting": {
      "owner": "reporting_migrator",
      "connectRoles": ["reporting_reader"],
      "schemas": {"reports": {"grants": {"reporting_reader": {"tables": ["SELECT"]}}}},
      "userSettings": {"reporting_migrator": {"statementTimeoutMs": 0}}
    }
  }
}`

type finding struct {
	Code    string
	Pointer string
}

func document(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(baseOptions), &value); err != nil {
		t.Fatalf("decode base options: %v", err)
	}
	return value
}

// set stores value at path, creating nothing: every parent must exist.
func set(t *testing.T, document map[string]any, value any, path ...string) {
	t.Helper()
	object := document
	for _, name := range path[:len(path)-1] {
		child, ok := object[name].(map[string]any)
		if !ok {
			t.Fatalf("no object at %s", name)
		}
		object = child
	}
	object[path[len(path)-1]] = value
}

func marshal(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode options: %v", err)
	}
	return data
}

func findings(t *testing.T, raw json.RawMessage) []finding {
	t.Helper()
	diagnostics, err := postgres.Validate(raw)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got := make([]finding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if file := diagnostic.Location().File(); file != "" {
			t.Errorf("%s carries file %q; pointers are options-relative", diagnostic.Code(), file)
		}
		got = append(got, finding{string(diagnostic.Code()), diagnostic.Location().Pointer()})
	}
	return got
}

func TestValidAcceptsSharedRuntimeUsersAndSeveralDatabases(t *testing.T) {
	t.Parallel()
	shared := document(t)
	set(t, shared, map[string]any{"purpose": "runtime", "passwordSecretRef": "app-api-password", "memberOf": []any{"app_writer"}}, "users", "app_api")
	set(t, shared, map[string]any{"purpose": "runtime", "passwordSecretRef": "app-worker-password", "memberOf": []any{"app_writer"}}, "users", "app_worker")
	set(t, shared, map[string]any{"app_api": map[string]any{"statementTimeoutMs": 5}, "app_worker": map[string]any{"lockTimeoutMs": 7}}, "databases", "application", "userSettings")

	manySchemas := document(t)
	set(t, manySchemas, map[string]any{
		"app":     map[string]any{"grants": map[string]any{"app_writer": map[string]any{"tables": []any{"SELECT"}}}},
		"billing": map[string]any{"grants": map[string]any{"app_writer": map[string]any{"sequences": []any{"USAGE"}}}},
	}, "databases", "application", "schemas")

	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{"base", []byte(baseOptions)},
		{"runtime users sharing a role", marshal(t, shared)},
		{"several schemas in one database", marshal(t, manySchemas)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := findings(t, test.raw); len(got) != 0 {
				t.Errorf("want no findings, got %v", got)
			}
		})
	}
}

func TestValidateReportsEachRuleWithItsStableCode(t *testing.T) {
	t.Parallel()
	member := func() any { return []any{"app_writer", "missing_role"} }
	tests := []struct {
		name   string
		change func(*testing.T, map[string]any)
		want   []finding
	}{
		{"user and role share a name", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"login": false}, "roles", "app_runtime")
		}, []finding{{"postgres.shared_role_name", "/roles/app_runtime"}}},
		{"unknown membership", func(t *testing.T, d map[string]any) {
			set(t, d, member(), "users", "app_runtime", "memberOf")
		}, []finding{{"postgres.unknown_member_role", "/users/app_runtime/memberOf/1"}}},
		{"unknown owner", func(t *testing.T, d map[string]any) {
			set(t, d, "missing_migrator", "databases", "application", "owner")
		}, []finding{
			{"postgres.unknown_owner", "/databases/application/owner"},
			{"postgres.settings_user_without_access", "/databases/application/userSettings/app_migrator"},
		}},
		{"unknown connection role", func(t *testing.T, d map[string]any) {
			set(t, d, []any{"app_writer", "missing_role"}, "databases", "application", "connectRoles")
		}, []finding{{"postgres.unknown_connect_role", "/databases/application/connectRoles/1"}}},
		{"unknown grantee", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"tables": []any{"SELECT"}}, "databases", "application", "schemas", "app", "grants", "missing_role")
		}, []finding{{"postgres.unknown_grantee", "/databases/application/schemas/app/grants/missing_role"}}},
		{"unknown settings user", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"lockTimeoutMs": 1}, "databases", "application", "userSettings", "missing_user")
		}, []finding{{"postgres.unknown_settings_user", "/databases/application/userSettings/missing_user"}}},
		{"runtime owner", func(t *testing.T, d map[string]any) {
			set(t, d, "app_runtime", "databases", "application", "owner")
		}, []finding{
			{"postgres.owner_not_migration_user", "/databases/application/owner"},
			{"postgres.settings_user_without_access", "/databases/application/userSettings/app_migrator"},
		}},
		{"migration user owns a second database", func(t *testing.T, d map[string]any) {
			set(t, d, "app_migrator", "databases", "reporting", "owner")
		}, []finding{
			{"postgres.migration_user_owns_multiple_databases", "/databases/reporting/owner"},
			{"postgres.settings_user_without_access", "/databases/reporting/userSettings/reporting_migrator"},
		}},
		{"grantee outside connectRoles", func(t *testing.T, d map[string]any) {
			set(t, d, []any{"reporting_reader"}, "databases", "application", "connectRoles")
		}, []finding{
			{"postgres.grantee_without_connect", "/databases/application/schemas/app/grants/app_writer"},
			{"postgres.settings_user_without_access", "/databases/application/userSettings/app_runtime"},
		}},
		{"settings for a user without access", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"lockTimeoutMs": 100}, "databases", "application", "userSettings", "reporting_app")
		}, []finding{{"postgres.settings_user_without_access", "/databases/application/userSettings/reporting_app"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := document(t)
			test.change(t, d)
			if got := findings(t, marshal(t, d)); !reflect.DeepEqual(got, test.want) {
				t.Errorf("findings:\nwant %v\ngot  %v", test.want, got)
			}
		})
	}
}

func TestValidateReportsOnlyTheRootCause(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*testing.T, map[string]any)
		want   []finding
	}{
		{"an unknown grantee is not also a grantee without connect", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"tables": []any{"SELECT"}}, "databases", "reporting", "schemas", "reports", "grants", "ghost")
		}, []finding{{"postgres.unknown_grantee", "/databases/reporting/schemas/reports/grants/ghost"}}},
		{"an unknown settings user is not also without access", func(t *testing.T, d map[string]any) {
			set(t, d, map[string]any{"lockTimeoutMs": 1}, "databases", "reporting", "userSettings", "ghost")
		}, []finding{{"postgres.unknown_settings_user", "/databases/reporting/userSettings/ghost"}}},
		{"one unknown owner on two databases is not shared ownership", func(t *testing.T, d map[string]any) {
			set(t, d, "ghost", "databases", "application", "owner")
			set(t, d, "ghost", "databases", "reporting", "owner")
			set(t, d, map[string]any{}, "databases", "application", "userSettings")
			set(t, d, map[string]any{}, "databases", "reporting", "userSettings")
		}, []finding{
			{"postgres.unknown_owner", "/databases/application/owner"},
			{"postgres.unknown_owner", "/databases/reporting/owner"},
		}},
		{"one runtime owner on two databases is not shared ownership", func(t *testing.T, d map[string]any) {
			set(t, d, "app_runtime", "databases", "application", "owner")
			set(t, d, "app_runtime", "databases", "reporting", "owner")
			set(t, d, map[string]any{}, "databases", "application", "userSettings")
			set(t, d, map[string]any{}, "databases", "reporting", "userSettings")
		}, []finding{
			{"postgres.owner_not_migration_user", "/databases/application/owner"},
			{"postgres.owner_not_migration_user", "/databases/reporting/owner"},
		}},
		{"a migration user owning three databases is reported on the second and third", func(t *testing.T, d map[string]any) {
			for _, name := range []string{"beta", "zeta"} {
				set(t, d, map[string]any{
					"owner": "app_migrator", "connectRoles": []any{"app_writer"},
					"schemas": map[string]any{"s": map[string]any{"grants": map[string]any{"app_writer": map[string]any{"tables": []any{"SELECT"}}}}},
				}, "databases", name)
			}
			set(t, d, "app_migrator", "databases", "reporting", "owner")
			set(t, d, map[string]any{}, "databases", "reporting", "userSettings")
		}, []finding{
			{"postgres.migration_user_owns_multiple_databases", "/databases/beta/owner"},
			{"postgres.migration_user_owns_multiple_databases", "/databases/reporting/owner"},
			{"postgres.migration_user_owns_multiple_databases", "/databases/zeta/owner"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := document(t)
			test.change(t, d)
			if got := findings(t, marshal(t, d)); !reflect.DeepEqual(got, test.want) {
				t.Errorf("findings:\nwant %v\ngot  %v", test.want, got)
			}
		})
	}
}

// TestValidateOrderDoesNotDependOnInputOrder writes the same options with every object's keys in a
// different order and requires the same findings in the same order (E2, TE7).
func TestValidateOrderDoesNotDependOnInputOrder(t *testing.T) {
	t.Parallel()
	d := document(t)
	set(t, d, map[string]any{"login": false}, "roles", "app_runtime")
	set(t, d, "ghost", "databases", "application", "owner")
	set(t, d, []any{"app_writer", "missing_role"}, "users", "app_runtime", "memberOf")
	set(t, d, "app_migrator", "databases", "reporting", "owner")
	set(t, d, map[string]any{"tables": []any{"SELECT"}}, "databases", "application", "schemas", "app", "grants", "missing_role")
	want := findings(t, marshal(t, d))
	if len(want) < 5 {
		t.Fatalf("the scenario should report several findings, got %v", want)
	}
	sorted := slices.IsSortedFunc(want, func(a, b finding) int { return strings.Compare(a.Pointer+a.Code, b.Pointer+b.Code) })
	if !sorted {
		t.Errorf("findings are not ordered by pointer and code: %v", want)
	}
	source := rand.New(rand.NewPCG(7, 11))
	for range 25 {
		var shuffled bytes.Buffer
		writeShuffled(&shuffled, source, d)
		if got := findings(t, shuffled.Bytes()); !reflect.DeepEqual(got, want) {
			t.Fatalf("order depends on input key order:\nwant %v\ngot  %v\ninput %s", want, got, shuffled.Bytes())
		}
	}
}

func writeShuffled(out *bytes.Buffer, source *rand.Rand, value any) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		source.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		out.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				out.WriteByte(',')
			}
			name, _ := json.Marshal(key)
			out.Write(name)
			out.WriteByte(':')
			writeShuffled(out, source, typed[key])
		}
		out.WriteByte('}')
	default:
		encoded, _ := json.Marshal(typed)
		out.Write(encoded)
	}
}

// TestMessagesNeverEchoOptionValues puts a canary in every value the rules read and requires that no
// message repeats it (S4).
func TestMessagesNeverEchoOptionValues(t *testing.T) {
	t.Parallel()
	const canary = "canary-7319"
	d := document(t)
	set(t, d, canary+"-owner", "databases", "application", "owner")
	set(t, d, []any{canary + "-role"}, "users", "app_runtime", "memberOf")
	set(t, d, []any{canary + "-connect"}, "databases", "reporting", "connectRoles")
	set(t, d, canary+"-secret-ref", "users", "app_migrator", "passwordSecretRef")
	set(t, d, "runtime", "users", "reporting_migrator", "purpose")
	diagnostics, err := postgres.Validate(marshal(t, d))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("the canary scenario reported nothing")
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message(), "canary") || strings.Contains(diagnostic.Location().Pointer(), "canary") {
			t.Errorf("%s repeats a value: %q at %s", diagnostic.Code(), diagnostic.Message(), diagnostic.Location().Pointer())
		}
	}
}

func TestValidateRefusesOptionsThatDoNotDecode(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"not an object":    `[]`,
		"unknown field":    `{"users": {}, "roles": {}, "databases": {}, "extra": 1}`,
		"unknown nested":   `{"users": {"a": {"purpose": "runtime", "passwordSecretRef": "x", "surprise": true}}, "roles": {}, "databases": {}}`,
		"trailing data":    `{"users": {}, "roles": {}, "databases": {}} {}`,
		"wrong type":       `{"users": [], "roles": {}, "databases": {}}`,
		"truncated":        `{"users": `,
		"empty input":      ``,
		"membership shape": `{"users": {"a": {"purpose": "runtime", "memberOf": "writer"}}, "roles": {}, "databases": {}}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := postgres.Validate(json.RawMessage(raw)); err == nil {
				t.Error("want an error for options that do not decode")
			}
		})
	}
}
