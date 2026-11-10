package packaged_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/tests/acceptance/packaged"
)

func artifactFile(t *testing.T, dir, name, content string) packaged.Artifact {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := packaged.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return packaged.Artifact{Path: path, SHA256: sum}
}

func good(t *testing.T) packaged.Manifest {
	dir := t.TempDir()
	return packaged.Manifest{
		CLI: artifactFile(t, dir, "cli", "binary"), SDK: artifactFile(t, dir, "sdk.tgz", "sdk"),
		Modules: map[string]packaged.Artifact{"postgres": artifactFile(t, dir, "postgres.tar.gz", "module")},
	}
}

func TestManifest_VerifiesAnIntactSet(t *testing.T) {
	t.Parallel()
	m := good(t)
	path := filepath.Join(t.TempDir(), "manifest.json")
	data, _ := json.Marshal(m)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := packaged.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Verify(); err != nil {
		t.Fatalf("an intact set was refused: %v", err)
	}
}

func TestManifest_RefusesAChangedByteAMissingFileAndASourceDirectory(t *testing.T) {
	t.Parallel()
	changed := good(t)
	if err := os.WriteFile(changed.SDK.Path, []byte("sdk!"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := good(t)
	if err := os.Remove(missing.CLI.Path); err != nil {
		t.Fatal(err)
	}
	tree := good(t)
	dir := t.TempDir()
	tree.Modules["postgres"] = packaged.Artifact{Path: dir, SHA256: tree.Modules["postgres"].SHA256}
	for name, tc := range map[string]struct {
		m    packaged.Manifest
		want string
	}{
		"changed byte":     {changed, "hashes to"},
		"missing file":     {missing, "no such file"},
		"source directory": {tree, "not a regular file"},
		"no modules":       {packaged.Manifest{}, "no module archive"},
	} {
		err := tc.m.Verify()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
}

func TestManifest_LoadRefusesAnUnknownFieldAndTrailingData(t *testing.T) {
	t.Parallel()
	valid, err := json.Marshal(good(t))
	if err != nil {
		t.Fatal(err)
	}
	withField := strings.Replace(string(valid), `{"cli"`, `{"clii":{},"cli"`, 1)
	for name, tc := range map[string]struct{ doc, want string }{
		"unknown field": {withField, "unknown field"},
		"trailing data": {string(valid) + `{"cli":{}}`, "trailing data"},
		"trailing junk": {string(valid) + " x", "trailing data"},
	} {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, []byte(tc.doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := packaged.Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
}

func TestArtifact_CopyVerifiedRunsOnlyTheHashedBytes(t *testing.T) {
	t.Parallel()
	a := artifactFile(t, t.TempDir(), "cli", "binary")
	dest, err := a.CopyVerified(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the copy has mode %v, want 0700", info.Mode().Perm())
	}
	if got, _ := packaged.HashFile(dest); got != a.SHA256 {
		t.Errorf("the copy hashes to %s, want %s", got, a.SHA256)
	}
	if err := os.WriteFile(a.Path, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CopyVerified(t.TempDir()); err == nil || !strings.Contains(err.Error(), "hashes to") {
		t.Errorf("a swapped file was copied: %v", err)
	}
}

func TestOwnedComposeProjects_ReturnsOnlyTheStacksOwnProjects(t *testing.T) {
	ls := []byte(`[{"Name":"sp-m01-090-ab12-env"},{"Name":"unrelated-app"},{"Name":"sp-m01-090-zz99-env"}]`)
	got, err := packaged.OwnedComposeProjects(ls, "sp-m01-090-ab12")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "sp-m01-090-ab12-env" {
		t.Errorf("owned projects = %v, want only sp-m01-090-ab12-env", got)
	}
	if got, _ := packaged.OwnedComposeProjects(ls, ""); len(got) != 0 {
		t.Errorf("an empty stack name owns %v, want nothing", got)
	}
	if _, err := packaged.OwnedComposeProjects([]byte("not json"), "x"); err == nil {
		t.Error("malformed docker output was accepted")
	}
}
