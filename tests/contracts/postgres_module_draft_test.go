package contracts_test

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"reflect"
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
		{"runtimes", "compose", "file"}, {"runtimes", "compose", "hostAccessFile"},
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

func TestPostgresComposeSource(t *testing.T) {
	t.Parallel()
	root := postgresModuleRoot(t)
	base := readPostgresDocument(t, filepath.Join(root, "content", "runtime", "compose.yaml"))
	if _, exists := base["name"]; exists {
		t.Fatal("Compose project identity must be selected by the caller")
	}
	service := documents.ObjectAt(t, map[string]any(base), []string{"services", "postgres"})
	image, _ := service["image"].(string)
	if !regexp.MustCompile(`^postgres:18\.[0-9]+-bookworm@sha256:[0-9a-f]{64}$`).MatchString(image) {
		t.Fatalf("expected a digest-pinned PostgreSQL 18 image, got %q", image)
	}
	for _, field := range []string{"ports", "container_name", "privileged", "network_mode"} {
		if _, exists := service[field]; exists {
			t.Fatalf("base service must not set %s", field)
		}
	}
	volume := documents.ObjectAt(t, map[string]any(base), []string{"volumes", "postgres_data"})
	if _, exists := volume["name"]; exists {
		t.Fatal("volume identity must remain scoped to the Compose project")
	}
	health := documents.ObjectAt(t, map[string]any(base), []string{"services", "postgres", "healthcheck"})
	wantProbe := []any{"CMD", "pg_isready", "-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "-d", "postgres"}
	if !reflect.DeepEqual(health["test"], wantProbe) {
		t.Fatalf("expected a TCP engine probe, got %v", health["test"])
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

// postgresTLSFlags is the server TLS flag text the engine's runtime truth (module.yaml) and the
// Compose draft must both carry, byte for byte.
const postgresTLSFlags = "-c ssl=on -c ssl_cert_file=/run/stackpuddle/tls/server.crt " +
	"-c ssl_key_file=/run/stackpuddle/tls/server.key -c hba_file=/run/stackpuddle/pg_hba.conf"

func TestPostgresTLSParity(t *testing.T) {
	t.Parallel()
	root := postgresModuleRoot(t)
	hba := "'local all all peer' 'hostnossl all all all reject' 'hostssl all all all scram-sha-256'"
	for _, file := range []string{"module.yaml", filepath.Join("content", "runtime", "compose.yaml")} {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, postgresTLSFlags) {
			t.Errorf("%s: missing the TLS flag text %q", file, postgresTLSFlags)
		}
		if !strings.Contains(text, hba) {
			t.Errorf("%s: pg_hba lines missing or out of order (reject must precede accept)", file)
		}
		if regexp.MustCompile(`(?m)'host all|'host\s`).MatchString(text) || strings.Contains(text, "hostssl all all all trust") {
			t.Errorf("%s: plaintext or trust host rule", file)
		}
	}
}
