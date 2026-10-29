package contracts_test

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const (
	postgresDefaultsPath   = "../../modules/postgres/content/configuration/defaults.yaml"
	postgresDocsPath       = "../../docs/modules/postgres.md"
	postgresComposeBase    = "../../modules/postgres/content/runtime/compose.yaml"
	postgresComposeHost    = "../../modules/postgres/content/runtime/compose.host.yaml"
	postgresTableHeader    = "| Configuration field | Compose input | Default |"
	postgresVariablePrefix = "STACKPUDDLE_POSTGRES_"
)

// defaultsRow is one row of the defaults table in docs/modules/postgres.md.
type defaultsRow struct {
	field, input, value string
}

// TestPostgresDefaultsHaveOneHome makes defaults.yaml the one home of the PostgreSQL defaults: the
// documented table and every Compose fallback still present must equal it.
func TestPostgresDefaultsHaveOneHome(t *testing.T) {
	t.Parallel()
	defaults := readDefaultLeaves(t)
	rows := readDefaultsTable(t)
	fallbacks := map[string]string{}
	for _, path := range []string{postgresComposeBase, postgresComposeHost} {
		for variable, value := range composeFallbacks(t, path) {
			fallbacks[variable] = value
		}
	}
	if problems := defaultsDrift(defaults, rows, fallbacks); len(problems) > 0 {
		t.Errorf("the PostgreSQL defaults differ from defaults.yaml:\n%s", strings.Join(problems, "\n"))
	}
}

// TestPostgresDefaultsDriftIsDetected proves the comparison fails for each kind of drift, so a
// passing TestPostgresDefaultsHaveOneHome is not vacuous.
func TestPostgresDefaultsDriftIsDetected(t *testing.T) {
	t.Parallel()
	defaults := map[string]string{"resources.memoryMiB": "1024", "hostAccess.enabled": "false"}
	rows := []defaultsRow{
		{"resources.memoryMiB", postgresVariablePrefix + "MEMORY_MIB", "1024"},
		{"hostAccess.enabled", "Select the host overlay only when true", "false"},
	}
	if problems := defaultsDrift(defaults, rows, map[string]string{postgresVariablePrefix + "MEMORY_MIB": "1024"}); len(problems) != 0 {
		t.Fatalf("consistent inputs reported drift: %v", problems)
	}
	tests := []struct {
		name      string
		defaults  map[string]string
		rows      []defaultsRow
		fallbacks map[string]string
		want      string
	}{
		{"documented value differs", defaults, []defaultsRow{{"resources.memoryMiB", "", "2048"}, rows[1]}, nil, "resources.memoryMiB"},
		{"leaf without a row", defaults, rows[:1], nil, "hostAccess.enabled"},
		{"row without a leaf", defaults, append(slices.Clone(rows), defaultsRow{"settings.ghost", "", "1"}), nil, "settings.ghost"},
		{"fallback differs", defaults, rows, map[string]string{postgresVariablePrefix + "MEMORY_MIB": "512"}, postgresVariablePrefix + "MEMORY_MIB"},
		{"fallback without a row", defaults, rows, map[string]string{postgresVariablePrefix + "GHOST": "1"}, postgresVariablePrefix + "GHOST"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			problems := defaultsDrift(test.defaults, test.rows, test.fallbacks)
			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, test.want) }) {
				t.Errorf("want a problem naming %q, got %v", test.want, problems)
			}
		})
	}
}

func defaultsDrift(defaults map[string]string, rows []defaultsRow, fallbacks map[string]string) []string {
	var problems []string
	documented := map[string]defaultsRow{}
	byVariable := map[string]defaultsRow{}
	for _, row := range rows {
		documented[row.field] = row
		if strings.HasPrefix(row.input, postgresVariablePrefix) {
			byVariable[row.input] = row
		}
		if _, ok := defaults[row.field]; !ok {
			problems = append(problems, fmt.Sprintf("docs row %q has no leaf in defaults.yaml", row.field))
		}
	}
	for field, value := range defaults {
		row, ok := documented[field]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("defaults.yaml leaf %q has no row in the docs table", field))
		case row.value != value:
			problems = append(problems, fmt.Sprintf("docs row %q says %q, defaults.yaml says %q", field, row.value, value))
		}
	}
	for variable, value := range fallbacks {
		row, ok := byVariable[variable]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("Compose fallback %s maps to no docs row", variable))
		case defaults[row.field] != value:
			problems = append(problems, fmt.Sprintf("Compose fallback %s is %q, defaults.yaml %q is %q", variable, value, row.field, defaults[row.field]))
		}
	}
	slices.Sort(problems)
	return problems
}

func readDefaultLeaves(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(postgresDefaultsPath)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("decode %s: %v", postgresDefaultsPath, err)
	}
	leaves := map[string]string{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		if object, ok := value.(map[string]any); ok {
			for key, child := range object {
				walk(strings.TrimPrefix(prefix+"."+key, "."), child)
			}
			return
		}
		leaves[prefix] = fmt.Sprint(value)
	}
	walk("", tree)
	if len(leaves) == 0 {
		t.Fatalf("%s has no defaults", postgresDefaultsPath)
	}
	return leaves
}

func readDefaultsTable(t *testing.T) []defaultsRow {
	t.Helper()
	data, err := os.ReadFile(postgresDocsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, postgresTableHeader)
	if start < 0 {
		t.Fatalf("%s has no defaults table headed %q", postgresDocsPath, postgresTableHeader)
	}
	var rows []defaultsRow
	for _, line := range lines[start+2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "| "), "|")
		if len(cells) != 3 {
			t.Fatalf("defaults table row %q does not have three cells", line)
		}
		rows = append(rows, defaultsRow{
			field: strings.Trim(strings.TrimSpace(cells[0]), "`"),
			input: strings.Trim(strings.TrimSpace(cells[1]), "`"),
			value: strings.TrimSpace(cells[2]),
		})
	}
	if len(rows) == 0 {
		t.Fatalf("the defaults table in %s has no rows", postgresDocsPath)
	}
	return rows
}

var composeFallback = regexp.MustCompile(`\$\{(` + postgresVariablePrefix + `[A-Z0-9_]+):-([^}]*)\}`)

// composeFallbacks returns the "${STACKPUDDLE_POSTGRES_*:-value}" fallbacks of a Compose file, which
// disappear once the engine's renderer supplies every value.
func composeFallbacks(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fallbacks := map[string]string{}
	for _, match := range composeFallback.FindAllStringSubmatch(string(data), -1) {
		fallbacks[match[1]] = match[2]
	}
	return fallbacks
}
