//go:build integration

// Package compose_test renders the PostgreSQL module's Compose files with the real Docker Compose
// CLI. It runs only through task test:integration, and fails by name when Compose is missing.
package compose_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/documents"
)

func TestPostgresComposeRendering(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("Docker Compose is required for the Compose rendering contract tests: docker is not on PATH: %v", err)
	}
	if output, err := exec.Command("docker", "compose", "version").CombinedOutput(); err != nil {
		t.Fatalf("Docker Compose is required for the Compose rendering contract tests: `docker compose version` failed: %v\n%s", err, output)
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
		document := documents.DecodeJSON(t, output)
		service := documents.ObjectAt(t, document, []string{"services", "postgres"})
		if ports, exists := service["ports"]; exists {
			t.Fatalf("base publishes ports: %v", ports)
		}
		environment := documents.ObjectAt(t, document, []string{"services", "postgres", "environment"})
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
		volume := documents.ObjectAt(t, document, []string{"volumes", "postgres_data"})
		if volume["name"] != "postgres-contract_postgres_data" {
			t.Fatal("volume is not project-scoped")
		}
		password := documents.ObjectAt(t, document, []string{"secrets", "postgres_admin"})
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
		document := documents.DecodeJSON(t, output)
		service := documents.ObjectAt(t, document, []string{"services", "postgres"})
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
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "modules", "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
