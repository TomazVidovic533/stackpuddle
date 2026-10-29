// Command pack builds the packaged set the acceptance journey runs on: it builds each module archive with
// artifacts.Build, hashes the CLI binary and the SDK tarball, and writes the manifest.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/artifacts"
	"github.com/TomazVidovic533/stackpuddle/tests/acceptance/packaged"
)

type modules map[string]string

func (m modules) String() string { return fmt.Sprint(map[string]string(m)) }
func (m modules) Set(v string) error {
	name, dir, ok := strings.Cut(v, "=")
	if !ok || name == "" || dir == "" {
		return fmt.Errorf("-module wants name=<dir>, got %q", v)
	}
	m[name] = dir
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pack:", err)
		os.Exit(1)
	}
}

func run() error {
	mods := modules{}
	cli := flag.String("cli", "", "built CLI binary")
	sdk := flag.String("sdk", "", "packed SDK tarball")
	out := flag.String("out", "", "manifest.json to write")
	flag.Var(mods, "module", "name=<module source dir> (repeatable)")
	flag.Parse()
	if *cli == "" || *sdk == "" || *out == "" || len(mods) == 0 {
		return fmt.Errorf("-cli, -sdk, -out and at least one -module are required")
	}
	outDir := filepath.Dir(*out)
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	m := packaged.Manifest{Modules: map[string]packaged.Artifact{}}
	var err error
	if m.CLI, err = artifact(*cli); err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	if m.SDK, err = artifact(*sdk); err != nil {
		return fmt.Errorf("sdk: %w", err)
	}
	for name, dir := range mods {
		var buf bytes.Buffer
		digest, err := artifacts.Build(artifacts.Module, dir, &buf)
		if err != nil {
			return fmt.Errorf("module %s: %w", name, err)
		}
		path := filepath.Join(outDir, name+".tar.gz")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			return err
		}
		a, err := artifact(path)
		if err != nil {
			return err
		}
		fmt.Printf("module %s archive digest %s (file sha256 %s)\n", name, digest, a.SHA256)
		m.Modules[name] = a
	}
	fmt.Printf("cli sha256 %s\nsdk sha256 %s\n", m.CLI.SHA256, m.SDK.SHA256)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, append(data, '\n'), 0o600)
}

func artifact(path string) (packaged.Artifact, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return packaged.Artifact{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return packaged.Artifact{}, err
	}
	if !info.Mode().IsRegular() {
		return packaged.Artifact{}, fmt.Errorf("%s is not a regular file", abs)
	}
	sum, err := packaged.HashFile(abs)
	return packaged.Artifact{Path: abs, SHA256: sum}, err
}
