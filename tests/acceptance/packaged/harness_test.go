//go:build acceptance

package packaged_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/artifacts"
	"github.com/TomazVidovic533/stackpuddle/tests/acceptance/packaged"
)

const (
	manifestEnv = "STACKPUDDLE_PACKAGED_MANIFEST"
	namePrefix  = "sp-m01-090-"
	stepBudget  = 90 * time.Second
	busybox     = "mirror.gcr.io/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"
)

// pack is the verified packaged set every test runs on. TestMain fills it or fails the package by name.
var pack packaged.Manifest

func TestMain(m *testing.M) {
	path := os.Getenv(manifestEnv)
	if path == "" {
		fmt.Fprintf(os.Stderr, "packaged: %s is not set; build the set with cmd/pack. This package never skips.\n", manifestEnv)
		os.Exit(1)
	}
	var err error
	if pack, err = packaged.Load(path); err == nil {
		err = pack.Verify()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "packaged: the manifest %s is not usable: %v\n", path, err)
		os.Exit(1)
	}
	for _, args := range [][]string{{"info"}, {"compose", "version"}} {
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "packaged: Docker is required and `docker %s` failed: %v\n%s\n", args[0], err, out)
			os.Exit(1)
		}
	}
	fmt.Printf("packaged identities: cli sha256 %s, sdk sha256 %s\n", pack.CLI.SHA256, pack.SDK.SHA256)
	for name, a := range pack.Modules {
		fmt.Printf("packaged identities: module %s archive sha256 %s\n", name, a.SHA256)
	}
	os.Exit(m.Run())
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type result struct {
	exit           int
	stdout, stderr string
}

// stack is one disposable project with its own state, cache and secrets key.
type stack struct {
	t          *testing.T
	name, dir  string
	state      string
	vars       []string
	tags       []string // local image tags this test created; the only tags it removes
	mu         sync.Mutex
	srv        *httptest.Server
	canarySeen bool
}

// newStack writes files (with {{NAME}} and {{SLOW}} replaced) into a fresh project directory.
func newStack(t *testing.T, files map[string]string) *stack {
	t.Helper()
	s := &stack{t: t, name: namePrefix + random(4), dir: t.TempDir(), state: t.TempDir()}
	for path, content := range files {
		content = strings.NewReplacer("{{NAME}}", s.name, "{{SLOW}}", "1").Replace(content)
		write(t, filepath.Join(s.dir, path), content)
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	s.vars = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "DOCKER_HOST=" + os.Getenv("DOCKER_HOST"),
		"STACKPUDDLE_STATE_DIR=" + s.state, "STACKPUDDLE_CACHE_DIR=" + t.TempDir(), "STACKPUDDLE_SECRETS_KEY=" + key}
	t.Cleanup(s.cleanup)
	return s
}

// dirFiles reads a testdata directory into a path-to-content map.
func dirFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// consumerFiles is the shared consumer fixture served from the packaged module archive and a bundle. Its
// module comes only from the archive named in the manifest: the lock carries the archive digest and the
// CLI fetches it over loopback HTTP and checks it before use. bundleModule is the module the bundle lists.
func (s *stack) consumerFiles(bundleModule string) map[string]string {
	t := s.t
	fixture, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "consumer", "stackpuddle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	module := pack.Modules["postgres"]
	moduleBytes, err := os.ReadFile(module.Path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := artifacts.Digest(bytes.NewReader(moduleBytes))
	if err != nil || digest != "sha256:"+module.SHA256 {
		t.Fatalf("the module archive digest %q (%v) is not the manifest's sha256:%s", digest, err, module.SHA256)
	}
	bundleDir := t.TempDir()
	write(t, filepath.Join(bundleDir, "bundle.yaml"), "apiVersion: stackpuddle/v1\nkind: Bundle\nversion: 0.1.0-alpha.1\nmodules:\n  "+bundleModule+":\n    version: 0.1.0-alpha.1\n")
	var bundle bytes.Buffer
	bundleDigest, err := artifacts.Build(artifacts.Bundle, bundleDir, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/bundle.tar.gz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bundle.Bytes()) })
	mux.HandleFunc("/postgres.tar.gz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(moduleBytes) })
	s.srv = httptest.NewServer(mux) // listens on 127.0.0.1 only
	t.Cleanup(s.srv.Close)
	lock := fmt.Sprintf("apiVersion: stackpuddle/v1\nkind: Lock\nbundle:\n  version: 0.1.0-alpha.1\n  source: %s/bundle.tar.gz\n  digest: %s\nmodules:\n  postgres:\n    version: 0.1.0-alpha.1\n    source: %s/postgres.tar.gz\n    format: tar+gzip\n    digest: %s\n",
		s.srv.URL, bundleDigest, s.srv.URL, digest)
	project := strings.Replace(string(fixture), "name: consumer", "name: "+s.name, 1)
	project = strings.Replace(project, "\nspec:\n", "\nspec:\n  manifest: stackpuddle.manifest.yaml\n  lock: stackpuddle.lock.yaml\n", 1)
	return map[string]string{
		"stackpuddle.yaml":          project,
		"stackpuddle.lock.yaml":     lock,
		"stackpuddle.manifest.yaml": "apiVersion: stackpuddle/v1\nkind: Manifest\nbundleVersion: 0.1.0-alpha.1\nmodules:\n  - postgres\n",
	}
}

func (s *stack) command(args ...string) *exec.Cmd {
	if !contains(args, "--env") {
		args = append(append([]string(nil), args...), "--env", "local")
	}
	cmd := exec.Command(pack.CLI.Path, args...)
	cmd.Dir, cmd.Env = s.dir, s.vars
	return cmd
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// proc is a running CLI process with its output captured as it arrives.
type proc struct {
	cmd          *exec.Cmd
	mu           sync.Mutex
	stdout, errb bytes.Buffer
	done         chan struct{}
	exit         int
}

type locked struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (l locked) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }

func (s *stack) start(group bool, args ...string) *proc {
	s.t.Helper()
	p := &proc{cmd: s.command(args...), done: make(chan struct{})}
	p.cmd.Stdout, p.cmd.Stderr = locked{&p.mu, &p.stdout}, locked{&p.mu, &p.errb}
	if group {
		p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := p.cmd.Start(); err != nil {
		s.t.Fatalf("start %v: %v", args, err)
	}
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		if ee, ok := err.(*exec.ExitError); ok {
			p.exit = ee.ExitCode()
		} else if err != nil {
			p.exit = 127
		}
		close(p.done)
	}()
	s.t.Cleanup(func() { _ = p.cmd.Process.Kill(); <-p.done })
	return p
}

func (p *proc) text() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stdout.String(), p.errb.String()
}

func (p *proc) wait(t *testing.T) result {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(stepBudget * 3):
		out, errs := p.text()
		t.Fatalf("the command did not finish\nstdout: %s\nstderr: %s", out, errs)
	}
	out, errs := p.text()
	return result{p.exit, out, errs}
}

// waitStderr returns the word after substr once the process has printed it (the operation ID after "accepted ").
func (p *proc) waitStderr(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(stepBudget)
	for time.Now().Before(deadline) {
		_, errs := p.text()
		if i := strings.Index(errs, substr); i >= 0 {
			rest := errs[i+len(substr):]
			if end := strings.IndexAny(rest, " ;\n"); end >= 0 {
				return rest[:end]
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%q never appeared on stderr", substr)
	return ""
}

func (s *stack) run(args ...string) result {
	s.t.Helper()
	r := s.start(false, args...).wait(s.t)
	if strings.Contains(r.stdout+r.stderr, "sp-canary") {
		s.canarySeen = true
	}
	return r
}

func (s *stack) mustRun(args ...string) result {
	s.t.Helper()
	r := s.run(args...)
	if r.exit != 0 {
		s.t.Fatalf("stackpuddle %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), r.exit, r.stdout, r.stderr)
	}
	return r
}

func (s *stack) json(args ...string) map[string]any {
	s.t.Helper()
	return decode(s.t, s.mustRun(append(args, "--output", "json")...).stdout)
}

func decode(t *testing.T, text string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("not a JSON document: %v\n%s", err, text)
	}
	return doc
}

// at walks a JSON document by keys and returns nil when a key is missing.
func at(doc map[string]any, keys ...string) any {
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func str(doc map[string]any, keys ...string) string {
	v, _ := at(doc, keys...).(string)
	return v
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// names lists containers, volumes and networks whose name carries this project's name.
func (s *stack) names() string {
	s.t.Helper()
	return strings.Join([]string{
		docker(s.t, "ps", "-a", "--filter", "name="+s.name, "--format", "{{.Names}}"),
		docker(s.t, "volume", "ls", "--filter", "name="+s.name, "--format", "{{.Name}}"),
		docker(s.t, "network", "ls", "--filter", "name="+s.name, "--format", "{{.Name}}"),
	}, "|")
}

func (s *stack) hasContainer(fragment string) bool {
	return strings.Contains(docker(s.t, "ps", "-a", "--filter", "name="+s.name, "--format", "{{.Names}}"), fragment)
}

func (s *stack) waitRunning(fragment string) {
	s.t.Helper()
	deadline := time.Now().Add(stepBudget)
	for time.Now().Before(deadline) {
		if strings.Contains(docker(s.t, "ps", "--filter", "name="+s.name, "--filter", "status=running", "--format", "{{.Names}}"), fragment) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("no running container named like %q", fragment)
}

// stateHash hashes every file under the state directory, so "state bytes unchanged" is one comparison.
func (s *stack) stateHash() string {
	s.t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(s.state, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(s.state, p)
		fmt.Fprintf(h, "%s|", rel)
		h.Write(data)
		return err
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// stateFiles returns the paths under the state directory whose base name matches pattern.
func (s *stack) stateFiles(pattern string) []string {
	var found []string
	_ = filepath.WalkDir(s.state, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if ok, _ := filepath.Match(pattern, d.Name()); ok {
				found = append(found, p)
			}
		}
		return nil
	})
	return found
}

// cleanup removes this project's objects with `docker compose -p <project> down -v` and the image tags this
// test created, and nothing else [RV8].
func (s *stack) cleanup() {
	out, err := s.command("inspect", "env/local", "--view", "state", "--output", "json").Output()
	var doc struct {
		ComposeProject string `json:"composeProject"`
	}
	if err == nil && json.Unmarshal(out, &doc) == nil && strings.Contains(doc.ComposeProject, s.name) {
		if o, err := exec.Command("docker", "compose", "-p", doc.ComposeProject, "down", "-v", "--timeout", "1").CombinedOutput(); err != nil {
			s.t.Errorf("cleanup: docker compose -p %s down -v: %v\n%s", doc.ComposeProject, err, o)
		}
	}
	for _, tag := range s.tags {
		_ = exec.Command("docker", "rmi", tag).Run()
	}
	if s.canarySeen {
		s.t.Error("a canary secret value appeared in CLI output")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// newPackaged builds a stack on the consumer fixture and the packaged module. edit may change the project
// files before they are written (a compatibility case breaks one thing).
func newPackaged(t *testing.T, bundleModule string, edit func(files map[string]string)) *stack {
	t.Helper()
	s := newStack(t, nil)
	files := s.consumerFiles(bundleModule)
	if edit != nil {
		edit(files)
	}
	for path, content := range files {
		write(t, filepath.Join(s.dir, path), content)
	}
	return s
}
