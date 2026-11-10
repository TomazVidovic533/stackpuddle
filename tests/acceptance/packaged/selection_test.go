//go:build acceptance

package packaged_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
	first, second := s.json("run", "report"), s.json("run", "report")
	for _, done := range []map[string]any{first, second} {
		if code, ok := at(done, "exitCode").(float64); !ok || code != 0 {
			t.Fatalf("run report = %v, want exitCode 0", done)
		}
	}
	if str(first, "invocation") == "" || str(first, "invocation") == str(second, "invocation") {
		t.Fatalf("two runs have invocations %q and %q, want two distinct IDs", str(first, "invocation"), str(second, "invocation"))
	}
	job := s.run("run", "broken")
	if job.exit != 10 || !strings.Contains(job.stdout+job.stderr, "job.exited_nonzero") {
		t.Fatalf("a failing job: exit %d, want 10 with job.exited_nonzero\n%s%s", job.exit, job.stdout, job.stderr)
	}
}

// failureCodes returns the stable codes a failed command printed as JSON: the error's own and each failed
// step's.
func failureCodes(t *testing.T, r result) []string {
	t.Helper()
	var doc struct {
		Error struct {
			Code      string `json:"code"`
			Operation struct {
				Steps []struct {
					Error *struct {
						Code string `json:"code"`
					} `json:"error"`
				} `json:"steps"`
			} `json:"operation"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &doc); err != nil {
		t.Fatalf("stderr is not a JSON failure document: %v\n%s", err, r.stderr)
	}
	codes := []string{doc.Error.Code}
	for _, step := range doc.Error.Operation.Steps {
		if step.Error != nil {
			codes = append(codes, step.Error.Code)
		}
	}
	return codes
}

func TestSelection_AJobWhoseImageIsAbsentFailsInTheRuntimeNotAsAJobFailure(t *testing.T) {
	s := groupsStack(t)
	s.mustRun("up", "--all", "--timeout", "3m")
	r := s.run("run", "absent", "--output", "json")
	if r.exit == 0 || r.exit == 10 {
		t.Fatalf("exit %d, want a failure that is neither success nor a job failure (10)\n%s%s", r.exit, r.stdout, r.stderr)
	}
	codes := failureCodes(t, r)
	if slices.Contains(codes, "job.exited_nonzero") {
		t.Fatalf("a missing image was reported as a job failure: %v", codes)
	}
	if !slices.Contains(codes, "runtime.image_missing") && !slices.Contains(codes, "runtime.effect_failed") {
		t.Fatalf("codes %v, want runtime.image_missing or runtime.effect_failed", codes)
	}
}
