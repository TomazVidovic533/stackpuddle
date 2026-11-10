package contracts_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const (
	postgresDefaultsPath = "../../modules/postgres/content/configuration/defaults.yaml"
	postgresDocsPath     = "../../docs/modules/postgres.md"
	postgresTableHeader  = "| Configuration field | Compose input | Default |"
)

// defaultsRow is one row of the defaults table in docs/modules/postgres.md.
type defaultsRow struct {
	field, input, value string
}

// TestPostgresDefaultsHaveOneHome makes defaults.yaml the one home of the PostgreSQL defaults: the
// documented table must equal it.
func TestPostgresDefaultsHaveOneHome(t *testing.T) {
	t.Parallel()
	defaults := readDefaultLeaves(t)
	rows := readDefaultsTable(t)
	if problems := defaultsDrift(defaults, rows); len(problems) > 0 {
		t.Errorf("the PostgreSQL defaults differ from defaults.yaml:\n%s", strings.Join(problems, "\n"))
	}
}

// TestPostgresDefaultsDriftIsDetected proves the comparison fails for each kind of drift, so a
// passing TestPostgresDefaultsHaveOneHome is not vacuous.
func TestPostgresDefaultsDriftIsDetected(t *testing.T) {
	t.Parallel()
	defaults := map[string]string{"resources.memoryMiB": "1024", "hostAccess.enabled": "false"}
	rows := []defaultsRow{
		{"resources.memoryMiB", "", "1024"},
		{"hostAccess.enabled", "Select the host overlay only when true", "false"},
	}
	if problems := defaultsDrift(defaults, rows); len(problems) != 0 {
		t.Fatalf("consistent inputs reported drift: %v", problems)
	}
	tests := []struct {
		name     string
		defaults map[string]string
		rows     []defaultsRow
		want     string
	}{
		{"documented value differs", defaults, []defaultsRow{{"resources.memoryMiB", "", "2048"}, rows[1]}, "resources.memoryMiB"},
		{"leaf without a row", defaults, rows[:1], "hostAccess.enabled"},
		{"row without a leaf", defaults, append(slices.Clone(rows), defaultsRow{"settings.ghost", "", "1"}), "settings.ghost"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			problems := defaultsDrift(test.defaults, test.rows)
			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, test.want) }) {
				t.Errorf("want a problem naming %q, got %v", test.want, problems)
			}
		})
	}
}

func defaultsDrift(defaults map[string]string, rows []defaultsRow) []string {
	var problems []string
	documented := map[string]defaultsRow{}
	for _, row := range rows {
		documented[row.field] = row
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
