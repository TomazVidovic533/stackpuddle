//go:build acceptance

package harness

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

const projectLabel = "label=com.docker.compose.project="

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("harness: docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Inventory is what one Compose project owns.
type Inventory struct {
	Containers, Volumes, Networks []string
}

// Count is the number of runtime objects in the inventory.
func (i Inventory) Count() int { return len(i.Containers) + len(i.Volumes) + len(i.Networks) }

// Inventory lists the containers, volumes and networks labelled with the Compose project.
func (s *Stack) Inventory(project string) Inventory {
	s.t.Helper()
	f := projectLabel + project
	return Inventory{
		Containers: lines(docker(s.t, "ps", "-a", "--filter", f, "--format", "{{.Names}}")),
		Volumes:    lines(docker(s.t, "volume", "ls", "--filter", f, "--format", "{{.Name}}")),
		Networks:   lines(docker(s.t, "network", "ls", "--filter", f, "--format", "{{.Name}}")),
	}
}

// Everything lists every object whose Compose project name carries this stack's disposable name, whether or
// not the CLI still knows the project. It is how a test shows that nothing was created.
func (s *Stack) Everything() []string {
	s.t.Helper()
	var out []string
	for _, kind := range [][]string{
		{"ps", "-a", "--format", "{{.Names}} {{.Label \"com.docker.compose.project\"}}"},
		{"volume", "ls", "--format", "{{.Name}} {{.Label \"com.docker.compose.project\"}}"},
		{"network", "ls", "--format", "{{.Name}} {{.Label \"com.docker.compose.project\"}}"},
	} {
		for _, l := range lines(docker(s.t, kind...)) {
			if strings.Contains(l, s.Name) {
				out = append(out, l)
			}
		}
	}
	return out
}

// Running reports whether a container of the project whose name carries fragment is running.
func (s *Stack) Running(project, fragment string) bool {
	s.t.Helper()
	out := docker(s.t, "ps", "--filter", projectLabel+project, "--filter", "status=running", "--format", "{{.Names}}")
	return strings.Contains(out, fragment)
}

// WaitRunning waits until a container of the project whose name carries fragment runs.
func (s *Stack) WaitRunning(project, fragment string) {
	s.t.Helper()
	deadline := time.Now().Add(StepBudget)
	for time.Now().Before(deadline) {
		if s.Running(project, fragment) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.t.Fatalf("harness: no running container named like %q in %s", fragment, project)
}

// Published lists the host port bindings of the project's containers, such as "127.0.0.1:15432->5432/tcp".
func (s *Stack) Published(project string) []string {
	s.t.Helper()
	var out []string
	for _, l := range lines(docker(s.t, "ps", "--filter", projectLabel+project, "--format", "{{.Ports}}")) {
		for _, p := range strings.Split(l, ", ") {
			if strings.Contains(p, "->") {
				out = append(out, p)
			}
		}
	}
	return out
}

// RemoveVolume removes exactly one named volume. It is what `docker volume prune -a` does to an unused
// volume; the suite never prunes the host [RV8].
func (s *Stack) RemoveVolume(name string) {
	s.t.Helper()
	docker(s.t, "volume", "rm", name)
}

// cleanup removes this stack's Compose projects with `docker compose -p <project> down -v` and nothing else:
// a project whose name does not carry the disposable name is never touched [RV8].
func (s *Stack) cleanup() {
	found := map[string]bool{}
	s.mu.Lock()
	for p := range s.projects {
		found[p] = true
	}
	s.mu.Unlock()
	for _, kind := range [][]string{
		{"ps", "-a", "--format", "{{.Label \"com.docker.compose.project\"}}"},
		{"volume", "ls", "--format", "{{.Label \"com.docker.compose.project\"}}"},
		{"network", "ls", "--format", "{{.Label \"com.docker.compose.project\"}}"},
	} {
		out, err := exec.Command("docker", kind...).Output()
		if err != nil {
			s.t.Errorf("cleanup: docker %s: %v", kind[0], err)
			continue
		}
		for _, p := range lines(strings.TrimSpace(string(out))) {
			if strings.Contains(p, s.Name) {
				found[p] = true
			}
		}
	}
	for p := range found {
		if !strings.Contains(p, s.Name) {
			continue
		}
		if o, err := exec.Command("docker", "compose", "-p", p, "down", "-v", "--timeout", "1").CombinedOutput(); err != nil {
			s.t.Errorf("cleanup: docker compose -p %s down -v: %v\n%s", p, err, o)
		}
	}
}
