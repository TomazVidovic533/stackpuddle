//go:build acceptance

package packaged_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func groupsStack(t *testing.T) *stack { return newStack(t, dirFiles(t, "testdata/groups")) }

func count(names, fragment string) int { return strings.Count(names, fragment) }

func TestSelection_OverlappingGroupsStartEachWorkloadOnce(t *testing.T) {
	s := groupsStack(t)
	s.mustRun("up", "app/web", "app/data", "--timeout", "3m")
	if n := count(s.names(), "worker"); n != 1 {
		t.Fatalf("worker appears %d times in %q, want once", n, s.names())
	}
	if !s.hasContainer("cache") || !s.hasContainer("api") {
		t.Fatalf("a group member is missing: %s", s.names())
	}
}

func TestSelection_ASharedResourceChangeReachingAnUnselectedConsumerNeedsExpandScope(t *testing.T) {
	s := groupsStack(t)
	s.mustRun("up", "--all", "--timeout", "3m")
	def := filepath.Join(s.dir, "definitions", "kv-store", "module.yaml")
	data, err := os.ReadFile(def)
	if err != nil {
		t.Fatal(err)
	}
	write(t, def, strings.Replace(string(data), `sleep, "3600"`, `sleep, "3601"`, 1))
	r := s.run("up", "module/primary", "--timeout", "3m")
	if r.exit != 3 || !strings.Contains(r.stderr+r.stdout, "selection.expanded_scope_required") {
		t.Fatalf("exit %d, want 3 with selection.expanded_scope_required\n%s%s", r.exit, r.stdout, r.stderr)
	}
	s.mustRun("up", "module/primary", "--expand-scope", "--timeout", "3m")
}

func TestSelection_UpAllRunsNoJobWithoutAPrerequisiteAndRunKeepsEachJobsOwnResult(t *testing.T) {
	s := groupsStack(t)
	s.mustRun("up", "--all", "--timeout", "3m")
	if s.hasContainer("report") || s.hasContainer("broken") {
		t.Fatalf("up --all ran a job that no workload requires: %s", s.names())
	}
	a, b := s.mustRun("run", "report", "--output", "json"), s.mustRun("run", "report", "--output", "json")
	if a.stdout == b.stdout {
		t.Fatalf("two runs of the same job printed the same document, want distinct IDs:\n%s", a.stdout)
	}
	job := s.run("run", "broken")
	if job.exit != 10 || !strings.Contains(job.stdout+job.stderr, "job.exited_nonzero") {
		t.Fatalf("a failing job: exit %d, want 10 with job.exited_nonzero\n%s%s", job.exit, job.stdout, job.stderr)
	}
	engine := s.run("run", "no-such-job")
	if engine.exit == 10 || strings.Contains(engine.stdout+engine.stderr, "job.exited_nonzero") {
		t.Fatalf("an engine-side failure was reported as a job failure: exit %d\n%s%s", engine.exit, engine.stdout, engine.stderr)
	}
}
