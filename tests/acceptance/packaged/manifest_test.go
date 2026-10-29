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
