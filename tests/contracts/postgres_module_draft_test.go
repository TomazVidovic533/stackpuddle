package contracts_test

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/artifacts"

	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/documents"
)

func TestPostgresModuleDraftReferences(t *testing.T) {
	t.Parallel()
	root := postgresModuleRoot(t)
	descriptor := readPostgresDocument(t, filepath.Join(root, "module.yaml"))
	for key, want := range map[string]string{
		"apiVersion": "stackpuddle/v1", "kind": "Module",
		"name": "postgres", "version": "0.1.0-alpha.1",
	} {
		if descriptor[key] != want {
			t.Fatalf("%s = %v, want %s", key, descriptor[key], want)
		}
	}
	if _, exists := descriptor["cliCompatibility"]; exists {
		t.Fatal("CLI compatibility needs a supporting implementation and tests")
	}
	references := [][]string{
		{"configuration", "schema"}, {"configuration", "defaults"},
		{"provisioning", "contract"},
	}
	for _, parts := range references {
		object := documents.ObjectAt(t, map[string]any(descriptor), parts[:len(parts)-1])
		reference, ok := object[parts[len(parts)-1]].(string)
		if !ok || !strings.HasPrefix(reference, "content/") || path.Clean(reference) != reference || strings.Contains(reference, "\\") {
			t.Fatalf("invalid reference at %s: %v", strings.Join(parts, "/"), reference)
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(reference)))
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("reference is not a regular file: %s", reference)
		}
	}
}

func TestPostgresModuleDraftArchive(t *testing.T) {
	t.Parallel()
	var archive bytes.Buffer
	if _, err := artifacts.Build(artifacts.Module, postgresModuleRoot(t), &archive); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.Inspect(artifacts.Module, &archive, artifacts.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

// postgresTLSFlags is the server TLS flag text module.yaml, the engine's runtime truth, must carry byte for byte.
const postgresTLSFlags = "-c ssl=on -c ssl_cert_file=/run/stackpuddle/tls/server.crt " +
	"-c ssl_key_file=/run/stackpuddle/tls/server.key -c hba_file=/run/stackpuddle/pg_hba.conf"

// postgresHBALine is the whole line that writes pg_hba.conf: nothing may come before or after the three rules.
const postgresHBALine = "printf '%s\\n' 'local all all peer' 'hostnossl all all all reject' " +
	"'hostssl all all all scram-sha-256' > /run/stackpuddle/pg_hba.conf"

var (
	postgresTrust     = regexp.MustCompile(`\btrust\b`)
	postgresPlaintext = regexp.MustCompile(`(?m)'host all|'host\s`)
	postgresSSLOff    = regexp.MustCompile(`ssl\s*=\s*(off|false|0|no)\b`)
)

// postgresPostureProblems lists every way module.yaml text departs from the TLS-only posture.
func postgresPostureProblems(text string) []string {
	var problems []string
	lines := strings.Split(text, "\n")
	hbaLines, tlsEnds := 0, 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == postgresHBALine {
			hbaLines++
		}
		if strings.HasSuffix(trimmed, postgresTLSFlags) {
			tlsEnds++
		}
	}
	if tlsEnds != 1 {
		problems = append(problems, "the TLS flags must end exactly one command line, with nothing after them")
	}
	if hbaLines != 1 {
		problems = append(problems, "the pg_hba line must be exactly the three rules in order (reject before accept)")
	}
	if postgresTrust.MatchString(text) {
		problems = append(problems, "trust token")
	}
	if postgresPlaintext.MatchString(text) {
		problems = append(problems, "plaintext host rule")
	}
	if postgresSSLOff.MatchString(text) {
		problems = append(problems, "ssl switched off")
	}
	return problems
}

func TestPostgresModuleTLSPosture(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(postgresModuleRoot(t), "module.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range postgresPostureProblems(string(raw)) {
		t.Errorf("module.yaml: %s", problem)
	}
}

func TestPostgresModuleTLSPostureRejectsWeakening(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(postgresModuleRoot(t), "module.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	good := string(raw)
	hba := strings.TrimPrefix(postgresHBALine, "printf '%s\\n' ")
	hba = strings.TrimSuffix(hba, " > /run/stackpuddle/pg_hba.conf")
	cases := map[string]string{
		"trust rule first":    strings.Replace(good, "'local all all peer'", "'hostssl all all 0.0.0.0/0 trust' 'local all all peer'", 1),
		"trust rule last":     strings.Replace(good, hba, hba+" 'host all all all trust'", 1),
		"ssl off after":       strings.Replace(good, postgresTLSFlags, postgresTLSFlags+" -c ssl=off", 1),
		"plaintext host":      strings.Replace(good, hba, hba+" 'host all all all scram-sha-256'", 1),
		"reject after accept": strings.Replace(good, "'hostnossl all all all reject' 'hostssl all all all scram-sha-256'", "'hostssl all all all scram-sha-256' 'hostnossl all all all reject'", 1),
	}
	if problems := postgresPostureProblems(good); len(problems) != 0 {
		t.Fatalf("shipped module.yaml has problems: %v", problems)
	}
	for name, text := range cases {
		if text == good {
			t.Fatalf("%s: mutation did not change the text", name)
		}
		if len(postgresPostureProblems(text)) == 0 {
			t.Errorf("%s: weakened module.yaml passes the posture check", name)
		}
	}
}

func postgresModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "modules", "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
