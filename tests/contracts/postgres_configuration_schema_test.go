package contracts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const postgresSchemaID = "urn:stackpuddle:module:postgres:configuration"

func TestPostgresConfigurationExamples(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	for _, name := range []string{"minimal.yaml", "multiple-databases.yaml", "multiple-users.yaml"} {
		t.Run(name, func(t *testing.T) {
			if err := schema.Validate(map[string]any(postgresFixture(t, name))); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresConfigurationDefaults(t *testing.T) {
	t.Parallel()
	defaults := readPostgresDocument(t, filepath.Join("..", "..", "modules", "postgres", "content", "configuration", "defaults.yaml"))
	sections := []string{"resources", "hostAccess", "startupTimeoutSeconds", "settings"}
	if len(defaults) != len(sections) {
		t.Fatalf("expected only instance defaults, got %v", defaults)
	}
	for _, name := range sections {
		value, ok := defaults[name]
		if !ok {
			t.Fatalf("missing default section %s", name)
		}
		if err := postgresSchema(t, "#/$defs/"+name).Validate(value); err != nil {
			t.Fatalf("invalid %s defaults: %v", name, err)
		}
	}
	hostAccess := objectAtPath(t, defaults, []string{"hostAccess"})
	if hostAccess["enabled"] != false {
		t.Fatal("host access must be disabled by default")
	}
	err := postgresSchema(t, "").Validate(map[string]any(defaults))
	if err == nil {
		t.Fatal("defaults must still require consumer database declarations")
	}
	for _, name := range []string{"users", "roles", "databases"} {
		assertRequiredFailure(t, err, "", name)
	}
}

func TestPostgresConfigurationRequiredFields(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	paths := [][]string{
		{"users"},
		{"roles"},
		{"databases"},
		{"hostAccess", "enabled"},
		{"hostAccess", "port"},
		{"users", "app_runtime", "purpose"},
		{"users", "app_runtime", "passwordSecretRef"},
		{"users", "app_runtime", "memberOf"},
		{"users", "app_migrator", "purpose"},
		{"users", "app_migrator", "passwordSecretRef"},
		{"roles", "app_writer", "login"},
		{"databases", "application", "owner"},
		{"databases", "application", "connectRoles"},
		{"databases", "application", "schemas"},
		{"databases", "application", "schemas", "app", "grants"},
	}
	for _, path := range paths {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			document := postgresFixture(t, "multiple-databases.yaml")
			removeContractField(t, document, path...)
			err := schema.Validate(map[string]any(document))
			if err == nil {
				t.Fatal("expected missing field to fail validation")
			}
			assertRequiredFailure(t, err, parentLocation(path), path[len(path)-1])
		})
	}
}

func TestPostgresConfigurationRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	tests := []struct {
		name    string
		path    []string
		value   any
		keyword string
	}{
		{"unknown root field", []string{"image"}, "postgres:latest", "additionalProperties"},
		{"outer instance wrapper", []string{"instances"}, map[string]any{}, "additionalProperties"},
		{"empty users", []string{"users"}, map[string]any{}, "minProperties"},
		{"empty roles", []string{"roles"}, map[string]any{}, "minProperties"},
		{"unknown user purpose", []string{"users", "app_runtime", "purpose"}, "admin", "enum"},
		{"empty memberships", []string{"users", "app_runtime", "memberOf"}, []any{}, "minItems"},
		{"duplicate memberships", []string{"users", "app_runtime", "memberOf"}, []any{"app_writer", "app_writer"}, "uniqueItems"},
		{"login permission role", []string{"roles", "app_writer", "login"}, true, "const"},
		{"nested role memberships", []string{"roles", "app_writer", "memberOf"}, []any{"reporting_reader"}, "additionalProperties"},
		{"permission role credentials", []string{"roles", "app_writer", "passwordSecretRef"}, "writer-password", "additionalProperties"},
		{"empty connect roles", []string{"databases", "application", "connectRoles"}, []any{}, "minItems"},
		{"duplicate connect roles", []string{"databases", "application", "connectRoles"}, []any{"app_writer", "app_writer"}, "uniqueItems"},
		{"empty schemas", []string{"databases", "application", "schemas"}, map[string]any{}, "minProperties"},
		{"empty grants", []string{"databases", "application", "schemas", "app", "grants"}, map[string]any{}, "minProperties"},
		{"schema ownership override", []string{"databases", "application", "schemas", "app", "owner"}, "app_runtime", "additionalProperties"},
		{"empty grant", []string{"databases", "application", "schemas", "app", "grants", "app_writer"}, map[string]any{}, "anyOf"},
		{"empty table privileges", []string{"databases", "application", "schemas", "app", "grants", "app_writer", "tables"}, []any{}, "minItems"},
		{"duplicate table privileges", []string{"databases", "application", "schemas", "app", "grants", "app_writer", "tables"}, []any{"SELECT", "SELECT"}, "uniqueItems"},
		{"empty sequence privileges", []string{"databases", "application", "schemas", "app", "grants", "app_writer", "sequences"}, []any{}, "minItems"},
		{"duplicate sequence privileges", []string{"databases", "application", "schemas", "app", "grants", "app_writer", "sequences"}, []any{"USAGE", "USAGE"}, "uniqueItems"},
		{"grant option", []string{"databases", "application", "schemas", "app", "grants", "app_writer", "grantOption"}, true, "additionalProperties"},
		{"empty databases", []string{"databases"}, map[string]any{}, "minProperties"},
		{"database list", []string{"databases"}, []any{}, "type"},
		{"small memory", []string{"resources", "memoryMiB"}, 255, "minimum"},
		{"fractional memory", []string{"resources", "memoryMiB"}, 256.5, "type"},
		{"zero cpu", []string{"resources", "cpus"}, 0, "exclusiveMinimum"},
		{"unknown resource", []string{"resources", "diskSize"}, "10GiB", "additionalProperties"},
		{"invalid enabled flag", []string{"hostAccess", "enabled"}, "true", "type"},
		{"zero port", []string{"hostAccess", "port"}, 0, "minimum"},
		{"large port", []string{"hostAccess", "port"}, 65536, "maximum"},
		{"public host address", []string{"hostAccess", "address"}, "0.0.0.0", "additionalProperties"},
		{"zero startup timeout", []string{"startupTimeoutSeconds"}, 0, "minimum"},
		{"large startup timeout", []string{"startupTimeoutSeconds"}, 3601, "maximum"},
		{"too few connections", []string{"settings", "maxConnections"}, 3, "minimum"},
		{"zero shared buffers", []string{"settings", "sharedBuffersMiB"}, 0, "minimum"},
		{"unknown engine setting", []string{"settings", "fsync"}, false, "additionalProperties"},
		{"instance timeout", []string{"settings", "statementTimeoutMs"}, 1000, "additionalProperties"},
		{"invalid database limit", []string{"databases", "application", "connectionLimit"}, -2, "minimum"},
		{"large database limit", []string{"databases", "application", "connectionLimit"}, 2147483648, "maximum"},
		{"negative timeout", []string{"databases", "application", "settings", "statementTimeoutMs"}, -1, "minimum"},
		{"large timeout", []string{"databases", "application", "settings", "lockTimeoutMs"}, 2147483648, "maximum"},
		{"string timeout", []string{"databases", "application", "settings", "lockTimeoutMs"}, "5s", "type"},
		{"fractional timeout", []string{"databases", "application", "settings", "statementTimeoutMs"}, 1.5, "type"},
		{"database server setting", []string{"databases", "application", "settings", "maxConnections"}, 100, "additionalProperties"},
		{"raw sql", []string{"databases", "application", "sql"}, "SELECT 1", "additionalProperties"},
		{"old access model", []string{"databases", "application", "access"}, map[string]any{}, "additionalProperties"},
		{"plaintext password", []string{"users", "app_runtime", "password"}, "example", "additionalProperties"},
		{"role privilege override", []string{"users", "app_runtime", "superuser"}, true, "additionalProperties"},
		{"empty secret reference", []string{"users", "app_runtime", "passwordSecretRef"}, "", "minLength"},
		{"secret file path", []string{"users", "app_runtime", "passwordSecretRef"}, "/tmp/password", "pattern"},
		{"secret reference object", []string{"users", "app_runtime", "passwordSecretRef"}, map[string]any{"value": "example"}, "type"},
		{"long secret reference", []string{"users", "app_runtime", "passwordSecretRef"}, strings.Repeat("a", 129), "maxLength"},
		{"uppercase owner reference", []string{"databases", "application", "owner"}, "Application", "pattern"},
		{"long owner reference", []string{"databases", "application", "owner"}, strings.Repeat("a", 64), "maxLength"},
		{"null owner reference", []string{"databases", "application", "owner"}, nil, "type"},
		{"runtime connection override", []string{"databases", "application", "userSettings", "app_runtime", "maxConnections"}, 20, "additionalProperties"},
		{"migration negative timeout", []string{"databases", "application", "userSettings", "app_migrator", "statementTimeoutMs"}, -1, "minimum"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := postgresFixture(t, "multiple-databases.yaml")
			setContractField(t, document, test.value, test.path...)
			err := schema.Validate(map[string]any(document))
			if err == nil {
				t.Fatal("expected invalid value to fail validation")
			}
			assertValidationFailure(t, err, jsonPointer(test.path), test.keyword)
		})
	}
}

func TestPostgresConfigurationRejectsMigrationMembership(t *testing.T) {
	t.Parallel()
	document := postgresFixture(t, "multiple-databases.yaml")
	setContractField(t, document, []any{"app_writer"}, "users", "app_migrator", "memberOf")
	err := postgresSchema(t, "").Validate(map[string]any(document))
	validationError, ok := err.(*jsonschema.ValidationError)
	if !ok {
		t.Fatalf("expected migration membership validation error, got %v", err)
	}
	var rejectsMembership func(*jsonschema.ValidationError) bool
	rejectsMembership = func(failure *jsonschema.ValidationError) bool {
		if _, ok := failure.ErrorKind.(*kind.Not); ok && jsonPointer(failure.InstanceLocation) == "/users/app_migrator" {
			return true
		}
		for _, cause := range failure.Causes {
			if rejectsMembership(cause) {
				return true
			}
		}
		return false
	}
	if !rejectsMembership(validationError) {
		t.Fatalf("expected membership prohibition at migration user, got %v", err)
	}
}

func TestPostgresConfigurationNames(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	invalidIdentifiers := []string{"", "Uppercase", "two-words", "1name", "pg_catalog", strings.Repeat("a", 64)}
	tests := []struct {
		name     string
		path     []string
		key      string
		reserved []string
	}{
		{"database", []string{"databases"}, "application", []string{"postgres", "template0", "template1"}},
		{"user", []string{"users"}, "app_runtime", []string{"postgres", "public", "none", "current_user", "session_user", "current_role"}},
		{"role", []string{"roles"}, "app_writer", []string{"postgres", "public", "none", "current_user", "session_user", "current_role"}},
		{"schema", []string{"databases", "application", "schemas"}, "app", []string{"information_schema"}},
		{"grant role", []string{"databases", "application", "schemas", "app", "grants"}, "app_writer", []string{"postgres", "public"}},
		{"user settings", []string{"databases", "application", "userSettings"}, "app_runtime", []string{"postgres", "public"}},
	}
	for _, test := range tests {
		for _, name := range append(append([]string{}, invalidIdentifiers...), test.reserved...) {
			t.Run(test.name+"/"+name, func(t *testing.T) {
				document := postgresFixture(t, "multiple-databases.yaml")
				object := objectAtPath(t, document, test.path)
				object[name] = object[test.key]
				delete(object, test.key)
				if err := schema.Validate(map[string]any(document)); err == nil {
					t.Fatalf("accepted invalid %s name %q", test.name, name)
				}
			})
		}
	}
	for _, path := range [][]string{
		{"databases", "application", "owner"},
		{"databases", "application", "connectRoles"},
		{"users", "app_runtime", "memberOf"},
	} {
		for _, name := range []string{"postgres", "public", "none", "current_user", "session_user", "current_role", "pg_admin"} {
			t.Run(strings.Join(path, "/")+"/"+name, func(t *testing.T) {
				document := postgresFixture(t, "multiple-databases.yaml")
				var value any = []any{name}
				if path[len(path)-1] == "owner" {
					value = name
				}
				setContractField(t, document, value, path...)
				if err := schema.Validate(map[string]any(document)); err == nil {
					t.Fatalf("accepted reserved role reference %q", name)
				}
			})
		}
	}
}

func TestPostgresConfigurationRejectsUnsafePrivileges(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	for _, scope := range []string{"users", "roles"} {
		name := "app_runtime"
		if scope == "roles" {
			name = "app_writer"
		}
		for _, attribute := range []string{"superuser", "createdb", "createrole", "replication", "bypassrls", "adminOption", "setRole"} {
			t.Run(scope+"/"+attribute, func(t *testing.T) {
				document := postgresFixture(t, "multiple-databases.yaml")
				path := []string{scope, name, attribute}
				setContractField(t, document, true, path...)
				assertValidationFailure(t, schema.Validate(map[string]any(document)), jsonPointer(path), "additionalProperties")
			})
		}
	}
	for _, target := range []string{"tables", "sequences"} {
		privileges := []string{"ALL", "CREATE", "TRUNCATE", "REFERENCES", "TRIGGER", "select", "SELECT; DROP TABLE assets"}
		if target == "sequences" {
			privileges = append(privileges, "UPDATE", "INSERT", "DELETE")
		}
		for _, privilege := range privileges {
			t.Run(target+"/"+privilege, func(t *testing.T) {
				document := postgresFixture(t, "multiple-databases.yaml")
				path := []string{"databases", "application", "schemas", "app", "grants", "app_writer", target}
				setContractField(t, document, []any{privilege}, path...)
				assertValidationFailure(t, schema.Validate(map[string]any(document)), jsonPointer(append(path, "0")), "enum")
			})
		}
	}
}

func TestPostgresConfigurationBoundaryValues(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	tests := []struct {
		name  string
		path  []string
		value any
	}{
		{"table-only grant", []string{"databases", "application", "schemas", "app", "grants", "app_writer"}, map[string]any{"tables": []any{"SELECT"}}},
		{"sequence-only grant", []string{"databases", "application", "schemas", "app", "grants", "app_writer"}, map[string]any{"sequences": []any{"USAGE"}}},
		{"minimum memory", []string{"resources", "memoryMiB"}, 256},
		{"fractional cpu", []string{"resources", "cpus"}, 0.25},
		{"minimum connections", []string{"settings", "maxConnections"}, 4},
		{"minimum shared buffers", []string{"settings", "sharedBuffersMiB"}, 1},
		{"minimum port", []string{"hostAccess", "port"}, 1},
		{"maximum port", []string{"hostAccess", "port"}, 65535},
		{"minimum startup timeout", []string{"startupTimeoutSeconds"}, 1},
		{"maximum startup timeout", []string{"startupTimeoutSeconds"}, 3600},
		{"unlimited database connections", []string{"databases", "application", "connectionLimit"}, -1},
		{"blocked database connections", []string{"databases", "application", "connectionLimit"}, 0},
		{"maximum database connections", []string{"databases", "application", "connectionLimit"}, 2147483647},
		{"disabled statement timeout", []string{"databases", "application", "settings", "statementTimeoutMs"}, 0},
		{"maximum lock timeout", []string{"databases", "application", "settings", "lockTimeoutMs"}, 2147483647},
		{"maximum role reference length", []string{"databases", "application", "owner"}, strings.Repeat("a", 63)},
		{"maximum secret reference length", []string{"users", "app_runtime", "passwordSecretRef"}, strings.Repeat("a", 128)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := postgresFixture(t, "multiple-databases.yaml")
			setContractField(t, document, test.value, test.path...)
			if err := schema.Validate(map[string]any(document)); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("disabled host access without port", func(t *testing.T) {
		document := postgresFixture(t, "multiple-databases.yaml")
		setContractField(t, document, false, "hostAccess", "enabled")
		removeContractField(t, document, "hostAccess", "port")
		if err := schema.Validate(map[string]any(document)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresConfigurationManyDatabases(t *testing.T) {
	t.Parallel()
	users := make(map[string]any)
	roles := make(map[string]any)
	databases := make(map[string]any)
	for index := 0; index < 100; index++ {
		name := fmt.Sprintf("app_%d", index)
		migrator, runtime, writer := name+"_migrator", name+"_runtime", name+"_writer"
		users[migrator] = map[string]any{
			"purpose":           "migrations",
			"passwordSecretRef": fmt.Sprintf("app-%d-migration-password", index),
		}
		users[runtime] = map[string]any{
			"purpose":           "runtime",
			"passwordSecretRef": fmt.Sprintf("app-%d-runtime-password", index),
			"memberOf":          []any{writer},
		}
		roles[writer] = map[string]any{"login": false}
		databases[name] = map[string]any{
			"owner":        migrator,
			"connectRoles": []any{writer},
			"schemas": map[string]any{
				"app": map[string]any{
					"grants": map[string]any{
						writer: map[string]any{"tables": []any{"SELECT", "INSERT", "UPDATE", "DELETE"}},
					},
				},
			},
		}
	}
	document := map[string]any{"users": users, "roles": roles, "databases": databases}
	if err := postgresSchema(t, "").Validate(document); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresConfigurationSchemaLeavesReferencesToSemanticValidation(t *testing.T) {
	t.Parallel()
	schema := postgresSchema(t, "")
	tests := []struct {
		name  string
		path  []string
		value any
	}{
		{"missing owner", []string{"databases", "application", "owner"}, "missing_migrator"},
		{"runtime owner", []string{"databases", "application", "owner"}, "app_runtime"},
		{"unknown membership", []string{"users", "app_runtime", "memberOf"}, []any{"missing_role"}},
		{"unknown connection role", []string{"databases", "application", "connectRoles"}, []any{"missing_role"}},
		{"grant without connect", []string{"databases", "application", "connectRoles"}, []any{"reporting_reader"}},
		{"unknown grant role", []string{"databases", "application", "schemas", "app", "grants"}, map[string]any{"missing_role": map[string]any{"tables": []any{"SELECT"}}}},
		{"unknown settings user", []string{"databases", "application", "userSettings"}, map[string]any{"missing_user": map[string]any{"statementTimeoutMs": 1}}},
		{"owner reused across databases", []string{"databases", "reporting", "owner"}, "app_migrator"},
		{"settings user without connection", []string{"databases", "application", "userSettings"}, map[string]any{"reporting_app": map[string]any{"lockTimeoutMs": 100}}},
		{"user role name collision", []string{"roles", "app_runtime"}, map[string]any{"login": false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := postgresFixture(t, "multiple-databases.yaml")
			setContractField(t, document, test.value, test.path...)
			if err := schema.Validate(map[string]any(document)); err != nil {
				t.Fatalf("schema must validate shape without claiming reference resolution: %v", err)
			}
		})
	}
}

func postgresSchema(t *testing.T, fragment string) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join("..", "..", "modules", "postgres", "content", "configuration", "schema.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(postgresSchemaID, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(postgresSchemaID + fragment)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func postgresFixture(t *testing.T, name string) contractDocument {
	t.Helper()
	return readPostgresDocument(t, filepath.Join("testdata", "modules", "postgres", "valid", name))
}

func readPostgresDocument(t *testing.T, path string) contractDocument {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document, ok := readFixtureYAML(t, contents).(map[string]any)
	if !ok {
		t.Fatalf("expected an object in %s", path)
	}
	return contractDocument(document)
}
