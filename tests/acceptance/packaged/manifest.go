// Package packaged proves the local journey from packaged artifacts only. The manifest names each artifact by
// path and sha256; a file that is missing, is a directory or hashes differently is refused, so a source tree
// never stands in for a packaged artifact.
package packaged

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
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
