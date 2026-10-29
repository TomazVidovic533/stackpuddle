package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/artifacts"
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
		object := objectAtPath(t, descriptor, parts[:len(parts)-1])
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
	service := objectAtPath(t, base, []string{"services", "postgres"})
	image, _ := service["image"].(string)
	if !regexp.MustCompile(`^postgres:18\.[0-9]+-bookworm@sha256:[0-9a-f]{64}$`).MatchString(image) {
		t.Fatalf("expected a digest-pinned PostgreSQL 18 image, got %q", image)
	}
	for _, field := range []string{"ports", "container_name", "privileged", "network_mode"} {
		if _, exists := service[field]; exists {
			t.Fatalf("base service must not set %s", field)
		}
	}
	volume := objectAtPath(t, base, []string{"volumes", "postgres_data"})
	if _, exists := volume["name"]; exists {
		t.Fatal("volume identity must remain scoped to the Compose project")
	}
	health := objectAtPath(t, base, []string{"services", "postgres", "healthcheck"})
	wantProbe := []any{"CMD", "pg_isready", "-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "-d", "postgres"}
	if !reflect.DeepEqual(health["test"], wantProbe) {
		t.Fatalf("expected a TCP engine probe, got %v", health["test"])
	}
}

func TestPostgresComposeRendering(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker Compose is required for rendering checks")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("Docker Compose is required for rendering checks")
	}
	root := postgresModuleRoot(t)
	temp := t.TempDir()
	secret := filepath.Join(temp, "admin-password")
	if err := os.WriteFile(secret, []byte("rendering-test-only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(temp, "empty.env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	render := func(overlay bool, values map[string]string) ([]byte, error) {
		args := []string{"compose", "--project-name", "postgres-contract", "--env-file", envFile,
			"-f", filepath.Join(root, "content", "runtime", "compose.yaml")}
		if overlay {
			args = append(args, "-f", filepath.Join(root, "content", "runtime", "compose.host.yaml"))
		}
		args = append(args, "config", "--format", "json")
		command := exec.Command("docker", args...)
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(key, "STACKPUDDLE_POSTGRES_") && !strings.HasPrefix(key, "COMPOSE_") {
				command.Env = append(command.Env, entry)
			}
		}
		for key, value := range values {
			command.Env = append(command.Env, key+"="+value)
		}
		return command.CombinedOutput()
	}
	t.Run("base", func(t *testing.T) {
		output, err := render(false, map[string]string{"STACKPUDDLE_POSTGRES_ADMIN_PASSWORD_FILE": secret})
		if err != nil {
			t.Fatalf("compose config: %v\n%s", err, output)
		}
		document := composeJSON(t, output)
		service := objectAtPath(t, document, []string{"services", "postgres"})
		if ports, exists := service["ports"]; exists {
			t.Fatalf("base publishes ports: %v", ports)
		}
		environment := objectAtPath(t, document, []string{"services", "postgres", "environment"})
		if _, exists := environment["POSTGRES_PASSWORD"]; exists {
			t.Fatal("inline administrator password")
		}
		if environment["POSTGRES_PASSWORD_FILE"] != "/run/secrets/postgres_admin_password" {
			t.Fatal("administrator file secret is not wired")
		}
		if environment["POSTGRES_INITDB_ARGS"] != "--auth-host=scram-sha-256 --auth-local=peer" {
			t.Fatal("unexpected bootstrap authentication settings")
		}
		if strings.Contains(string(output), "rendering-test-only") {
			t.Fatal("render exposed secret content")
		}
		mounts, ok := service["volumes"].([]any)
		if !ok || len(mounts) != 1 {
			t.Fatalf("unexpected volumes: %v", service["volumes"])
		}
		mount, ok := mounts[0].(map[string]any)
		if !ok || mount["type"] != "volume" || mount["source"] != "postgres_data" || mount["target"] != "/var/lib/postgresql" {
			t.Fatalf("unexpected PostgreSQL data mount: %v", mounts)
		}
		volume := objectAtPath(t, document, []string{"volumes", "postgres_data"})
		if volume["name"] != "postgres-contract_postgres_data" {
			t.Fatal("volume is not project-scoped")
		}
		password := objectAtPath(t, document, []string{"secrets", "postgres_admin"})
		if password["file"] != secret {
			t.Fatal("unexpected secret source")
		}
	})
	t.Run("custom settings and localhost", func(t *testing.T) {
		output, err := render(true, map[string]string{
			"STACKPUDDLE_POSTGRES_ADMIN_PASSWORD_FILE": secret,
			"STACKPUDDLE_POSTGRES_HOST_PORT":           "55432",
			"STACKPUDDLE_POSTGRES_MEMORY_MIB":          "2048", "STACKPUDDLE_POSTGRES_CPUS": "1.5",
			"STACKPUDDLE_POSTGRES_MAX_CONNECTIONS": "150", "STACKPUDDLE_POSTGRES_SHARED_BUFFERS_MIB": "256",
		})
		if err != nil {
			t.Fatalf("compose config: %v\n%s", err, output)
		}
		document := composeJSON(t, output)
		service := objectAtPath(t, document, []string{"services", "postgres"})
		wantCommand := []any{"postgres", "-c", "max_connections=150", "-c", "shared_buffers=256MB", "-c", "password_encryption=scram-sha-256"}
		if !reflect.DeepEqual(service["command"], wantCommand) {
			t.Fatalf("unexpected command: %v", service["command"])
		}
		if service["cpus"] != 1.5 {
			t.Fatalf("unexpected CPU limit: %v", service["cpus"])
		}
		if service["mem_limit"] != "2147483648" {
			t.Fatalf("unexpected memory limit: %v", service["mem_limit"])
		}
		ports, ok := service["ports"].([]any)
		if !ok || len(ports) != 1 {
			t.Fatalf("unexpected ports: %v", service["ports"])
		}
		port, ok := ports[0].(map[string]any)
		if !ok || port["host_ip"] != "127.0.0.1" || port["published"] != "55432" || port["target"] != float64(5432) {
			t.Fatalf("unexpected host binding: %v", ports)
		}
	})
	t.Run("missing administrator secret", func(t *testing.T) {
		output, err := render(false, nil)
		if err == nil || !strings.Contains(string(output), "STACKPUDDLE_POSTGRES_ADMIN_PASSWORD_FILE") {
			t.Fatalf("expected missing secret reference error: %v\n%s", err, output)
		}
	})
}

func postgresModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "modules", "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func composeJSON(t *testing.T, data []byte) contractDocument {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode Compose model: %v", err)
	}
	return contractDocument(document)
}
