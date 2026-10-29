//go:build acceptance

// Package harness runs the lifecycle rows of the consumer acceptance suite through the built command-line tool.
// It takes a fixture folder and knows no module by name: everything a row needs (environments, jobs, the
// persistent object, the module sources) comes from the folder's fixture.json [A5].
package harness

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// StepBudget bounds one wait for a visible effect (a step, a container).
const StepBudget = 3 * time.Minute

// ArchiveSource is where a module archive comes from: a module source dir built with artifacts.Build, or a
// ready file plus the digest it must have.
type ArchiveSource struct {
	Dir    string
	File   string
	Digest string
}

// Config is one run of the harness over one fixture folder.
type Config struct {
	// CLI is the absolute path of the built binary (STACKPUDDLE_CLI).
	CLI string
	// Fixture is the folder holding fixture.json, the project file and the unsupported project.
	Fixture string
	// Archives overrides the module source named in fixture.json, by module name.
	Archives map[string]ArchiveSource
}

// Module is one packaged module the fixture's project uses.
type Module struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Source is a module source dir, relative to the fixture folder.
	Source string `json:"source"`
}

// Environment is one environment of the fixture's project. PortSlot picks the {{PORT_n}} placeholder it
// exposes its export on.
type Environment struct {
	Name     string `json:"name"`
	PortSlot int    `json:"portSlot"`
}

// Edit is the source edit made after a kill's operation was accepted, to a workload the kill did not select.
type Edit struct {
	File      string `json:"file"`
	Old       string `json:"old"`
	New       string `json:"new"`
	Container string `json:"container"`
}

// Fixture is the parsed fixture.json. The harness reads only these keys.
type Fixture struct {
	Modules      []Module      `json:"modules"`
	Environments []Environment `json:"environments"`
	// SeedJob writes the row QueryJob reads; QueryJob exits 0 only on an authenticated verify-full connection
	// that reads the row back.
	SeedJob  string `json:"seedJob"`
	QueryJob string `json:"queryJob"`
	// DeniedJobs each exit 0 only when the database refused the operation with the SQLSTATE the job names.
	DeniedJobs []string `json:"deniedJobs"`
	// Persistent is the object whose data volume survives stop and remove.
	Persistent string `json:"persistent"`
	// Consumers are container name fragments that must run after up.
	Consumers []string `json:"consumers"`
	// KillRefs is the selection of the up that is killed.
	KillRefs []string `json:"killRefs"`
	Edit     Edit     `json:"edit"`
	// Unsupported is a folder (relative to the fixture) of a project that requires an unsupported capability.
	Unsupported string `json:"unsupported"`
}

// Env returns the environment at index i.
func (f Fixture) Env(i int) Environment { return f.Environments[i] }

// Folders lists the module fixture folders under root (each holds a fixture.json).
func Folders(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("harness: cannot list fixtures in %s: %v", root, err)
	}
	var out []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), "fixture.json")); e.IsDir() && err == nil {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	if len(out) == 0 {
		t.Fatalf("harness: no fixture folder under %s", root)
	}
	return out
}

// Run runs row once per fixture folder under root, each as its own subtest on its own disposable stack.
func Run(t *testing.T, root string, row func(t *testing.T, s *Stack)) {
	t.Helper()
	cli := os.Getenv("STACKPUDDLE_CLI")
	if cli == "" || !filepath.IsAbs(cli) {
		t.Fatalf("harness: STACKPUDDLE_CLI must be the absolute path of the built binary, got %q. This suite never skips.", cli)
	}
	for _, folder := range Folders(t, root) {
		t.Run(filepath.Base(folder), func(t *testing.T) {
			row(t, Open(t, Config{CLI: cli, Fixture: folder}))
		})
	}
}

// Open builds a disposable project from the fixture, serves its module archives and registers cleanup.
func Open(t *testing.T, cfg Config) *Stack {
	t.Helper()
	if info, err := os.Stat(cfg.CLI); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("harness: the CLI %q is not an executable file: %v", cfg.CLI, err)
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Fatalf("harness: Docker is required and `docker info` failed: %v\n%s", err, out)
	}
	var fx Fixture
	data, err := os.ReadFile(filepath.Join(cfg.Fixture, "fixture.json"))
	if err == nil {
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.DisallowUnknownFields()
		err = dec.Decode(&fx)
	}
	if err != nil || len(fx.Environments) < 2 || len(fx.Modules) == 0 {
		t.Fatalf("harness: %s/fixture.json is not usable (needs modules and two environments): %v", cfg.Fixture, err)
	}
	s := &Stack{t: t, cfg: cfg, Fixture: fx, Name: "sp-m01-070-" + revision() + "-" + random(4), Dir: t.TempDir(), State: t.TempDir(), Cache: t.TempDir()}
	key := base64.StdEncoding.EncodeToString([]byte(random(16))) // 32 bytes, fresh per run
	s.vars = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"STACKPUDDLE_STATE_DIR=" + s.State, "STACKPUDDLE_CACHE_DIR=" + s.Cache, "STACKPUDDLE_SECRETS_KEY=" + key}
	for _, name := range []string{"DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT"} {
		if v := os.Getenv(name); v != "" {
			s.vars = append(s.vars, name+"="+v)
		}
	}
	s.serve()
	s.writeProject()
	t.Cleanup(s.cleanup)
	return s
}

func revision() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return "norev"
	}
	return strings.TrimSpace(string(out))
}

func (s *Stack) replacer() *strings.Replacer {
	pairs := []string{"{{NAME}}", s.Name}
	for i, p := range s.ports {
		pairs = append(pairs, fmt.Sprintf("{{PORT_%d}}", i+1), fmt.Sprint(p))
	}
	return strings.NewReplacer(pairs...)
}

// copyTree writes every file under src (except fixture.json and the unsupported folder at top level) into dst
// with placeholders replaced.
func (s *Stack) copyTree(src, dst string, skip func(rel string) bool) {
	t := s.t
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if skip != nil && skip(rel) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dst, rel), s.replacer().Replace(string(data)))
	})
	if err != nil {
		t.Fatalf("harness: copy %s: %v", src, err)
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (s *Stack) writeProject() {
	s.copyTree(s.cfg.Fixture, s.Dir, func(rel string) bool {
		return rel == "fixture.json" || rel == "README.md" || (s.Fixture.Unsupported != "" && strings.HasPrefix(rel, s.Fixture.Unsupported+string(filepath.Separator)))
	})
	root := filepath.Join(s.Dir, "stackpuddle.yaml")
	data, err := os.ReadFile(root)
	if err != nil {
		s.t.Fatalf("harness: the fixture has no stackpuddle.yaml: %v", err)
	}
	project := strings.Replace(string(data), "\nspec:\n", "\nspec:\n  manifest: stackpuddle.manifest.yaml\n  lock: stackpuddle.lock.yaml\n", 1)
	var names []string
	for _, m := range s.Fixture.Modules {
		names = append(names, "  - "+m.Name)
	}
	for path, content := range map[string]string{
		"stackpuddle.yaml":          project,
		"stackpuddle.lock.yaml":     s.lock,
		"stackpuddle.manifest.yaml": "apiVersion: stackpuddle/v1\nkind: Manifest\nbundleVersion: " + s.Fixture.Modules[0].Version + "\nmodules:\n" + strings.Join(names, "\n") + "\n",
	} {
		if err := writeFile(filepath.Join(s.Dir, path), content); err != nil {
			s.t.Fatal(err)
		}
	}
}

// UnsupportedDir writes the fixture's unsupported project into a fresh folder and returns it.
func (s *Stack) UnsupportedDir() string {
	s.t.Helper()
	if s.Fixture.Unsupported == "" {
		s.t.Fatal("harness: the fixture names no unsupported project")
	}
	dir := s.t.TempDir()
	s.copyTree(filepath.Join(s.cfg.Fixture, s.Fixture.Unsupported), dir, nil)
	return dir
}
