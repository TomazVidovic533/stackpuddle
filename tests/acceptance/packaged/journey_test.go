//go:build acceptance

package packaged_test

import "testing"

// The 070 lifecycle rows run from manifest identities only: the CLI is pack.CLI.Path, the module is the
// archive the lock names by digest, and no source tree is on the path. TestMain has already verified every
// hash and printed the identities for the evidence.
func TestJourney_PackagedCLIAndModuleArchiveRunTheLifecycle(t *testing.T) {
	s := newPackaged(t, "postgres", nil)
	t.Logf("cli sha256 %s; module archive sha256 %s; sdk sha256 %s", pack.CLI.SHA256, pack.Modules["postgres"].SHA256, pack.SDK.SHA256)

	up := s.json("up", "--timeout", "5m")
	if str(up, "operation", "status") != "succeeded" {
		t.Fatalf("up = %v, want a succeeded operation (the migration job ran with verify-full)", up["operation"])
	}
	first := str(up, "operation", "id")

	again := s.json("up", "--timeout", "5m")
	if str(again, "operation", "status") != "succeeded" || str(again, "operation", "id") == first {
		t.Fatalf("a repeat up = %v, want a new succeeded operation", again["operation"])
	}

	one, two := s.json("run", "migrate"), s.json("run", "migrate")
	if str(one, "id") == "" && str(one, "operation", "id") == "" {
		t.Fatalf("run printed no invocation ID: %v", one)
	}
	if string(mustJSON(one)) == string(mustJSON(two)) {
		t.Fatalf("two runs printed the same document, want two IDs: %v", one)
	}

	live := s.run("check", "--live")
	if live.exit != 0 {
		t.Fatalf("check --live: exit %d\n%s%s", live.exit, live.stdout, live.stderr)
	}
	s.mustRun("stop", "--all")
	if r := s.run("destroy", "env/local"); r.exit == 0 && !s.hasVolumeKept() {
		t.Fatalf("destroy without --delete-data removed the data volume")
	}
	s.mustRun("destroy", "env/local", "--delete-data")
}

func (s *stack) hasVolumeKept() bool {
	return docker(s.t, "volume", "ls", "--filter", "name="+s.name, "--format", "{{.Name}}") != ""
}
