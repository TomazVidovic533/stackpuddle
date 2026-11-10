// Package packaged proves the local journey from packaged artifacts only. The manifest names each artifact by
// path and sha256; a file that is missing, is a directory or hashes differently is refused, so a source tree
// never stands in for a packaged artifact.
package packaged

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Artifact is one packaged file and the sha256 it must have.
type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest names the CLI binary, the SDK tarball and each module archive.
type Manifest struct {
	CLI     Artifact            `json:"cli"`
	SDK     Artifact            `json:"sdk"`
	Modules map[string]Artifact `json:"modules"`
}

// Load reads a manifest file. It does not verify the artifacts; Verify does.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	// A misspelt key or a second document must fail loudly: a silently ignored field would leave an
	// artifact unverified.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("decode manifest %s: trailing data after the manifest document", path)
	}
	return m, nil
}

// Verify checks that every artifact exists, is a regular file and hashes to its sha256.
func (m Manifest) Verify() error {
	if len(m.Modules) == 0 {
		return errors.New("manifest names no module archive")
	}
	if err := m.CLI.verify("cli"); err != nil {
		return err
	}
	if err := m.SDK.verify("sdk"); err != nil {
		return err
	}
	names := make([]string, 0, len(m.Modules))
	for name := range m.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := m.Modules[name].verify("module " + name); err != nil {
			return err
		}
	}
	return nil
}

func (a Artifact) verify(label string) error {
	if a.Path == "" || a.SHA256 == "" {
		return fmt.Errorf("%s: path and sha256 are both required", label)
	}
	info, err := os.Stat(a.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: %s is not a regular file; a source tree cannot stand in for a packaged artifact", label, a.Path)
	}
	got, err := HashFile(a.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if got != a.SHA256 {
		return fmt.Errorf("%s: %s hashes to %s, the manifest says %s", label, a.Path, got, a.SHA256)
	}
	return nil
}

// CopyVerified copies the artifact into dir as an owner-only executable and returns the copy's path. It hashes
// the bytes it reads once and copies those same bytes, so a file swapped after Verify cannot be run: the copy
// is re-hashed before it is returned.
func (a Artifact) CopyVerified(dir string) (string, error) {
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != a.SHA256 {
		return "", fmt.Errorf("%s hashes to %s, the manifest says %s", a.Path, got, a.SHA256)
	}
	dest := filepath.Join(dir, filepath.Base(a.Path))
	if err := os.WriteFile(dest, data, 0o700); err != nil {
		return "", err
	}
	if got, err := HashFile(dest); err != nil || got != a.SHA256 {
		return "", fmt.Errorf("the copy %s does not hash to %s (%v)", dest, a.SHA256, err)
	}
	return dest, nil
}

// HashFile returns the lowercase hex sha256 of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OwnedComposeProjects returns the names in `docker compose ls --format json` output that start with name, the
// unique name of one test stack. Cleanup removes only these, so a foreign project, or another stack of the
// same suite, is never touched [RV8].
func OwnedComposeProjects(lsJSON []byte, name string) ([]string, error) {
	var projects []struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(lsJSON, &projects); err != nil {
		return nil, fmt.Errorf("decode docker compose ls output: %w", err)
	}
	owned := []string{}
	for _, p := range projects {
		if name != "" && strings.HasPrefix(p.Name, name) {
			owned = append(owned, p.Name)
		}
	}
	return owned, nil
}
