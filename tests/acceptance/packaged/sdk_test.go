//go:build acceptance

package packaged_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const sdkScript = `
import { defineProject, encodeModel, port, secretProviders, secretRef, service } from "@stackpuddle/sdk";
import { environment } from "@stackpuddle/sdk/compose";
import { database, instance, migrationUser, permissionRole, runtimeUser, schema } from "@stackpuddle/sdk/postgres";

const api = service("api", {
  image: "ghcr.io/example/api:1.4.2",
  environment: { LOG_LEVEL: "info" },
  listeners: { http: { port: port(8080), protocol: "http" } },
});
const writer = permissionRole("writer");
const migrator = migrationUser("migrator", { password: secretRef("migrator-password") });
const primary = instance("primary", {
  users: [runtimeUser("app", { password: secretRef("app-password"), memberOf: [writer] }), migrator],
  roles: [writer],
  databases: [
    database("shop", {
      owner: migrator,
      connectRoles: [writer],
      schemas: [schema("public", [{ role: writer, tables: ["SELECT", "INSERT"], sequences: ["USAGE"] }])],
    }),
  ],
});
const local = environment("local", { context: "desktop-linux", secrets: secretProviders.local });
const result = encodeModel(defineProject("shop", { declarations: [api, primary], environments: [local] }));
if (!result.ok) {
  throw new Error(JSON.stringify(result.error));
}
process.stdout.write(result.value);
`

// The packaged SDK tarball, installed into a scratch project, writes the model of the YAML fixture it
// mirrors, byte for byte.
func TestSDK_PackedTarballWritesTheCanonicalModelOfTheYAMLFixture(t *testing.T) {
	scratch := t.TempDir()
	write(t, filepath.Join(scratch, "package.json"), `{"name":"scratch","private":true,"type":"module"}`)
	write(t, filepath.Join(scratch, ".npmrc"), "save-exact=true\n")
	write(t, filepath.Join(scratch, "smoke.mjs"), sdkScript)
	add := exec.Command("pnpm", "add", pack.SDK.Path, "--ignore-workspace", "--prefer-offline", "--ignore-scripts")
	add.Dir = scratch
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("pnpm add %s: %v\n%s", pack.SDK.Path, err, out)
	}
	node := exec.Command("node", "smoke.mjs")
	node.Dir = scratch
	emitted, err := node.Output()
	if err != nil {
		t.Fatalf("node smoke.mjs: %v", err)
	}

	definition, diags := model.LoadModuleDefinition(os.DirFS(filepath.Join("..", "..", "..", "modules", "postgres")))
	if len(diags) > 0 {
		t.Fatalf("modules/postgres does not load: %v", diags)
	}
	cases := filepath.Join("..", "..", "conformance", "cases", "parity", "plain-declarations")
	project, diags := model.Load(os.DirFS(cases), "stackpuddle.yaml", []model.ModuleDefinition{definition})
	if len(diags) > 0 {
		t.Fatalf("the YAML fixture does not load: %v", diags)
	}
	want := model.Encode(project)
	if !bytes.Equal(bytes.TrimSpace(emitted), bytes.TrimSpace(want)) {
		t.Fatalf("the SDK model differs from the canonical model of the YAML fixture\nsdk:  %s\nyaml: %s", emitted, want)
	}
}
