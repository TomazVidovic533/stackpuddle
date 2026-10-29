//go:build acceptance

package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stack is one disposable project: its own folder, state, cache, secrets key and Compose projects.
type Stack struct {
	t       *testing.T
	cfg     Config
	Fixture Fixture
	Name    string
	Dir     string
	State   string
	Cache   string
	Server  *Server
	vars    []string
	ports   []int
	lock    string
	mu      sync.Mutex
	// projects are the Compose projects seen for this stack; cleanup removes these and nothing else [RV8].
	projects map[string]bool
}

// Result is one finished command.
type Result struct {
	Exit           int
	Stdout, Stderr string
}

func (r Result) String() string {
	return fmt.Sprintf("exit %d\nstdout: %s\nstderr: %s", r.Exit, r.Stdout, r.Stderr)
}

// Port is the host port the environment at index i exposes on.
func (s *Stack) Port(i int) int { return s.ports[s.Fixture.Environments[i].PortSlot-1] }

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Stack) command(dir string, extra []string, args []string) *exec.Cmd {
	if !has(args, "--env") {
		args = append(append([]string(nil), args...), "--env", s.Fixture.Environments[0].Name)
	}
	cmd := exec.Command(s.cfg.CLI, args...)
	cmd.Dir, cmd.Env = dir, append(append([]string(nil), s.vars...), extra...)
	return cmd
}

// Proc is a running command with its output captured as it arrives.
type Proc struct {
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

// Start runs the CLI in the project folder without waiting. The process is killed at cleanup.
func (s *Stack) Start(args ...string) *Proc { return s.start(s.Dir, nil, args) }

func (s *Stack) start(dir string, extra, args []string) *Proc {
	s.t.Helper()
	p := &Proc{cmd: s.command(dir, extra, args), done: make(chan struct{})}
	p.cmd.Stdout, p.cmd.Stderr = locked{&p.mu, &p.stdout}, locked{&p.mu, &p.errb}
	if err := p.cmd.Start(); err != nil {
		s.t.Fatalf("harness: start %v: %v", args, err)
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

// Kill sends SIGKILL, which the CLI cannot handle, and waits for the process to be gone.
func (p *Proc) Kill(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		t.Fatal("harness: the process had already exited, so the kill proves nothing")
	default:
	}
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-p.done
}

// Exited reports whether the process has finished.
func (p *Proc) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Proc) text() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stdout.String(), p.errb.String()
}

// Wait returns the finished command.
func (p *Proc) Wait(t *testing.T) Result {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(StepBudget * 5):
		out, errs := p.text()
		t.Fatalf("harness: the command did not finish\nstdout: %s\nstderr: %s", out, errs)
	}
	out, errs := p.text()
	return Result{p.exit, out, errs}
}

// WaitStderr returns the word after substr once the process has printed it (the operation ID after "accepted ").
func (p *Proc) WaitStderr(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(StepBudget)
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
	_, errs := p.text()
	t.Fatalf("harness: %q never appeared on stderr: %s", substr, errs)
	return ""
}

// Run runs the CLI to completion in the project folder.
func (s *Stack) Run(args ...string) Result {
	s.t.Helper()
	return s.start(s.Dir, nil, args).Wait(s.t)
}

// RunWith runs the CLI with extra environment entries (a different DOCKER_HOST, for example).
func (s *Stack) RunWith(extra []string, args ...string) Result {
	s.t.Helper()
	return s.start(s.Dir, extra, args).Wait(s.t)
}

// RunAt runs the CLI in another folder (the unsupported project).
func (s *Stack) RunAt(dir string, args ...string) Result {
	s.t.Helper()
	return s.start(dir, nil, args).Wait(s.t)
}

// Must runs the CLI and fails the test unless it exits 0.
func (s *Stack) Must(args ...string) Result {
	s.t.Helper()
	r := s.Run(args...)
	if r.Exit != 0 {
		s.t.Fatalf("harness: stackpuddle %s: %s", strings.Join(args, " "), r)
	}
	return r
}

// JSON runs the CLI with --output json and decodes stdout. Exit must be 0.
func (s *Stack) JSON(args ...string) map[string]any {
	s.t.Helper()
	return Decode(s.t, s.Must(append(append([]string(nil), args...), "--output", "json")...).Stdout)
}

// Decode parses a JSON document.
func Decode(t *testing.T, text string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("harness: not a JSON document: %v\n%s", err, text)
	}
	return doc
}

// At walks a JSON document by keys and returns nil when a key is missing.
func At(doc map[string]any, keys ...string) any {
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

// Str is At as a string, "" when missing.
func Str(doc map[string]any, keys ...string) string {
	v, _ := At(doc, keys...).(string)
	return v
}

// Up runs `up env/<env>` and requires a succeeded operation. It returns the operation document.
func (s *Stack) Up(env string) map[string]any {
	s.t.Helper()
	doc := s.JSON("up", "env/"+env, "--env", env, "--timeout", "10m")
	if Str(doc, "operation", "status") != "succeeded" {
		s.t.Fatalf("harness: up env/%s = %v, want a succeeded operation", env, doc["operation"])
	}
	return At(doc, "operation").(map[string]any)
}

// OperationSteps returns the steps of an operation document.
func OperationSteps(op map[string]any) []map[string]any {
	var out []map[string]any
	steps, _ := op["steps"].([]any)
	for _, st := range steps {
		if m, ok := st.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Project returns the Compose project of an environment from `inspect --view state` and remembers it for cleanup.
func (s *Stack) Project(env string) string {
	s.t.Helper()
	doc := s.JSON("inspect", "env/"+env, "--view", "state", "--env", env)
	p := Str(doc, "composeProject")
	if !strings.Contains(p, s.Name) {
		s.t.Fatalf("harness: the Compose project %q of env/%s does not carry the disposable name %q", p, env, s.Name)
	}
	s.remember(p)
	return p
}

func (s *Stack) remember(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projects == nil {
		s.projects = map[string]bool{}
	}
	s.projects[p] = true
}

// StateObjects returns ref -> state of the realized objects the state view shows for ref in env.
func (s *Stack) StateObjects(ref, env string) map[string]string {
	s.t.Helper()
	doc := s.JSON("inspect", ref, "--view", "state", "--env", env)
	out := map[string]string{}
	objs, _ := doc["objects"].([]any)
	for _, o := range objs {
		if m, ok := o.(map[string]any); ok {
			out[Str(m, "ref")] = Str(m, "state")
		}
	}
	return out
}

// Fingerprint hashes every file under the state directory, so "nothing was written" is one comparison.
func (s *Stack) Fingerprint() string {
	s.t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(s.State, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(s.State, p)
		fmt.Fprintf(h, "%s|", rel)
		h.Write(data)
		return err
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SecretStore lists the sealed credential files (name and size). A new password changes a size.
func (s *Stack) SecretStore() []string {
	s.t.Helper()
	var out []string
	_ = filepath.WalkDir(s.State, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".sealed") {
			info, _ := d.Info()
			out = append(out, fmt.Sprintf("%s:%d", d.Name(), info.Size()))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// ReadProject reads a file of the project folder.
func (s *Stack) ReadProject(rel string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Dir, rel))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

// WriteProject replaces a file of the project folder.
func (s *Stack) WriteProject(rel, content string) {
	s.t.Helper()
	if err := writeFile(filepath.Join(s.Dir, rel), content); err != nil {
		s.t.Fatal(err)
	}
}
