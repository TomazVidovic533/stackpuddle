//go:build acceptance

// Package acceptance proves the Docker lifecycle through the built command-line tool, one test per Done row of
// SP-M01-070, over every fixture folder under modules/. Every row runs once per fixture on a disposable
// project; none skips (a missing binary or daemon fails by name in the harness).
package acceptance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/tests/acceptance/internal/harness"
)

func rows(t *testing.T, row func(t *testing.T, s *harness.Stack)) { harness.Run(t, "modules", row) }

// seeded brings the first environment up and seeds it, returning its Compose project.
func seeded(t *testing.T, s *harness.Stack, env string) string {
	t.Helper()
	s.Up(env)
	project := s.Project(env)
	for _, c := range s.Fixture.Consumers {
		s.WaitRunning(project, c)
	}
	s.Must("run", s.Fixture.SeedJob, "--env", env)
	return project
}

func TestAuthenticatedQueryOverVerifiedTLSFromTheConsumersNetwork(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		seeded(t, s, env)
		// The query job exits 0 only over TLS (pg_stat_ssl) with sslmode=verify-full and the seeded row.
		r := s.Must("run", s.Fixture.QueryJob, "--env", env)
		if strings.TrimSpace(r.Stdout) == "" {
			t.Fatalf("run printed nothing, want the invocation: %s", r)
		}
	})
}

func TestDeniedOperationsAreRejectedNotTimedOut(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		seeded(t, s, env)
		for _, job := range s.Fixture.DeniedJobs {
			start := time.Now()
			if r := s.Run("run", job, "--env", env); r.Exit != 0 {
				t.Errorf("%s: want the database's SQLSTATE rejection (job exit 0), got %s", job, r)
			}
			if time.Since(start) > 2*time.Minute {
				t.Errorf("%s took %s, a rejection must not look like a timeout", job, time.Since(start))
			}
		}
	})
}

func TestRepeatUpKeepsDataCredentialsAndPins(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		first := s.Inventory(project)
		secrets, lock := s.SecretStore(), s.ReadProject("stackpuddle.lock.yaml")
		if len(secrets) == 0 {
			t.Fatal("up left no sealed credential store")
		}
		again := s.Up(env)
		if harness.Str(again, "id") == "" {
			t.Fatalf("repeat up = %v, want an operation", again)
		}
		if got := s.Inventory(project); strings.Join(got.Volumes, ",") != strings.Join(first.Volumes, ",") || len(got.Containers) != len(first.Containers) {
			t.Fatalf("repeat up changed the runtime objects: before %+v after %+v", first, got)
		}
		if got := strings.Join(s.SecretStore(), ","); got != strings.Join(secrets, ",") {
			t.Fatalf("repeat up changed the credential store (a new password?): before %v after %s", secrets, got)
		}
		if s.ReadProject("stackpuddle.lock.yaml") != lock {
			t.Fatal("repeat up rewrote the lock: a silent pin change")
		}
		s.Must("run", s.Fixture.QueryJob, "--env", env) // the seeded row and the old credentials still work
	})
}

// A kill -9 mid-up leaves an interrupted operation; the rerun names it, observes the uncertain step before it
// retries, creates no second object, and leaves an unselected source edit unapplied.
func TestKillMidUpThenRerunObservesBeforeRetry(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		args := append([]string{"up"}, s.Fixture.KillRefs...)
		args = append(args, "--env", env, "--timeout", "10m")
		p := s.Start(args...)
		id := p.WaitStderr(t, "accepted ")
		// Wait for the first persisted step, then edit the unselected workload and kill.
		deadline := time.Now().Add(harness.StepBudget)
		for {
			if p.Exited() {
				t.Fatal("up finished before the first step could be observed; the row cannot prove a mid-operation kill")
			}
			op := harness.Decode(t, s.Must("inspect", "operation/"+id, "--env", env, "--output", "json").Stdout)
			done := false
			for _, st := range harness.OperationSteps(op) {
				done = done || harness.Str(st, "status") == "completed"
			}
			if done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no step completed within %s", harness.StepBudget)
			}
			time.Sleep(50 * time.Millisecond)
		}
		e := s.Fixture.Edit
		source := s.ReadProject(e.File)
		if !strings.Contains(source, e.Old) {
			t.Fatalf("%s lacks %q, so the unselected edit cannot be made", e.File, e.Old)
		}
		s.WriteProject(e.File, strings.Replace(source, e.Old, e.New, 1))
		p.Kill(t)

		old := harness.Decode(t, s.Must("inspect", "operation/"+id, "--env", env, "--output", "json").Stdout)
		var uncertain map[string]any
		for _, st := range harness.OperationSteps(old) {
			if harness.Str(st, "status") == "started" && uncertain == nil {
				uncertain = st
			}
		}
		project := s.Project(env)
		before := s.Inventory(project)
		secretsBefore := s.SecretStore()

		rerun := s.Run(append(append([]string{"up"}, s.Fixture.KillRefs...), "--env", env, "--timeout", "10m", "--output", "json")...)
		if rerun.Exit != 0 {
			t.Fatalf("the rerun failed: %s", rerun)
		}
		doc := harness.Decode(t, rerun.Stdout)
		next := harness.At(doc, "operation").(map[string]any)
		if harness.Str(next, "continues") != id || harness.Str(next, "id") == id {
			t.Fatalf("rerun operation = %v, want a new operation continuing %s", next, id)
		}
		oldAfter := harness.Decode(t, s.Must("inspect", "operation/"+id, "--env", env, "--output", "json").Stdout)
		if harness.Str(oldAfter, "status") != "interrupted" {
			t.Fatalf("the killed operation reads %q, want interrupted", harness.Str(oldAfter, "status"))
		}
		if uncertain == nil {
			t.Logf("no step was in flight at the kill; the observation row is vacuous for this timing")
		} else {
			ok := false
			for _, st := range harness.OperationSteps(next) {
				if harness.Str(st, "action") == harness.Str(uncertain, "action") && harness.Str(st, "object") == harness.Str(uncertain, "object") {
					status := harness.Str(st, "status")
					ok = status == "completed-by-observation" || status == "completed"
				}
			}
			if !ok {
				t.Fatalf("the uncertain step %v was not observed by the rerun: %v", uncertain, next["steps"])
			}
		}
		after := s.Inventory(project)
		if len(after.Volumes) < len(before.Volumes) || len(after.Networks) < len(before.Networks) || dup(after.Containers) || dup(after.Volumes) {
			t.Fatalf("duplicate or lost objects: before %+v after %+v", before, after)
		}
		if len(secretsBefore) > 0 && !prefixKept(secretsBefore, s.SecretStore()) {
			t.Fatalf("the credential store changed shape across the rerun: %v then %v", secretsBefore, s.SecretStore())
		}
		if s.Running(project, e.Container) || strings.Contains(strings.Join(after.Containers, ","), e.Container) {
			t.Fatalf("the unselected workload %q was created or changed by the rerun: %+v", e.Container, after)
		}
	})
}

func dup(names []string) bool {
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			return true
		}
		seen[n] = true
	}
	return false
}

// prefixKept: a store file may grow when a later step adds a credential, but it must not disappear.
func prefixKept(before, after []string) bool {
	names := map[string]bool{}
	for _, a := range after {
		names[a[:strings.LastIndex(a, ":")]] = true
	}
	for _, b := range before {
		if !names[b[:strings.LastIndex(b, ":")]] {
			return false
		}
	}
	return true
}

func TestTwoEnvironmentsKeepSeparateResourcesPortsDataAndTeardown(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		a, b := s.Fixture.Env(0).Name, s.Fixture.Env(1).Name
		pa := seeded(t, s, a)
		s.Up(b)
		pb := s.Project(b)
		if pa == pb {
			t.Fatalf("both environments use the Compose project %q", pa)
		}
		ia, ib := s.Inventory(pa), s.Inventory(pb)
		for _, v := range ia.Volumes {
			if contains(ib.Volumes, v) {
				t.Fatalf("volume %s is shared", v)
			}
		}
		if len(s.SecretStore()) < 2 {
			t.Fatalf("want one sealed credential store per environment, got %v", s.SecretStore())
		}
		if s.Port(0) == s.Port(1) {
			t.Fatal("harness gave both environments one port")
		}
		for _, c := range s.Fixture.Consumers {
			if !s.Running(pa, c) {
				t.Fatalf("selecting %s stopped %s in %s", b, c, a)
			}
		}
		// The seeded data lives only in the first environment.
		if r := s.Run("run", s.Fixture.QueryJob, "--env", b); r.Exit != 10 {
			t.Fatalf("query in %s: exit %d, want 10 (no migrated row; data was not migrated)\n%s", b, r.Exit, r)
		}
		s.Must("destroy", "env/"+b, "--env", b, "--delete-data", "--timeout", "10m")
		if got := s.Inventory(pb); got.Count() != 0 {
			t.Fatalf("destroy of %s left %+v", b, got)
		}
		if got := s.Inventory(pa); strings.Join(got.Volumes, ",") != strings.Join(ia.Volumes, ",") {
			t.Fatalf("destroy of %s touched %s: %+v", b, a, got)
		}
		s.Must("run", s.Fixture.QueryJob, "--env", a)
	})
}

func TestStopKeepsDataRemoveEditsDeclarationDestroyNeedsDeleteIntent(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		volumes := s.Inventory(project).Volumes
		if len(volumes) == 0 {
			t.Fatal("up created no volume")
		}
		s.Must("stop", "--all", "--env", env, "--timeout", "10m")
		if got := s.Inventory(project).Volumes; strings.Join(got, ",") != strings.Join(volumes, ",") {
			t.Fatalf("stop changed the volumes: %v then %v", volumes, got)
		}
		// remove edits the declaration only: the volumes stay and the YAML loses the workload.
		fx := s.Fixture
		before := s.ReadProject("stackpuddle.yaml")
		// Bindings still name the workload, so removing it would leave an invalid project: refused, nothing changed.
		if r := s.Run("remove", "workload/"+fx.Edit.Container, "--env", env); r.Exit == 0 || !strings.Contains(r.Stdout+r.Stderr, "remove.leaves_invalid_project") {
			t.Fatalf("remove of a bound workload: %s, want a refusal with remove.leaves_invalid_project", r)
		}
		if s.ReadProject("stackpuddle.yaml") != before {
			t.Fatal("a refused remove edited the declaration")
		}
		if got := s.Inventory(project).Volumes; strings.Join(got, ",") != strings.Join(volumes, ",") {
			t.Fatalf("a refused remove changed runtime volumes: %v then %v", volumes, got)
		}
		s.WriteProject("stackpuddle.yaml", withoutBindings(before, "workload/"+fx.Edit.Container))
		unbound := s.ReadProject("stackpuddle.yaml")
		s.Must("remove", "workload/"+fx.Edit.Container, "--env", env)
		if s.ReadProject("stackpuddle.yaml") == unbound {
			t.Fatal("remove did not edit the declaration")
		}
		if got := s.Inventory(project).Volumes; strings.Join(got, ",") != strings.Join(volumes, ",") {
			t.Fatalf("remove changed runtime volumes: %v then %v", volumes, got)
		}
		s.Run("destroy", "env/"+env, "--env", env, "--timeout", "10m")
		if got := s.Inventory(project).Volumes; strings.Join(got, ",") != strings.Join(volumes, ",") {
			t.Fatalf("destroy without --delete-data deleted data: %v then %v", volumes, got)
		}
		s.Must("destroy", "env/"+env, "--env", env, "--delete-data", "--timeout", "10m")
		if got := s.Inventory(project); got.Count() != 0 {
			t.Fatalf("destroy --delete-data left %+v", got)
		}
	})
}

// withoutBindings drops every list item under spec.bindings whose consumer is the given ref. A binding runs
// from its "- consumer:" line to the next list item or the next two-space key.
func withoutBindings(yaml, consumer string) string {
	var out []string
	dropping := false
	for _, line := range strings.Split(yaml, "\n") {
		switch {
		case strings.HasPrefix(line, "    - consumer: "):
			dropping = strings.TrimPrefix(line, "    - consumer: ") == consumer
		case line != "" && !strings.HasPrefix(line, "     "):
			dropping = false
		}
		if !dropping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func TestLostDataVolumeShowsLostAndUpRefusesToRecreateItEmpty(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		s.Must("stop", "--all", "--env", env, "--timeout", "10m")
		volumes := s.Inventory(project).Volumes
		if len(volumes) == 0 {
			t.Fatal("the stopped environment has no volume to lose")
		}
		// One volume, named: what `docker volume prune -a` does to an unused volume. No prune is ever run.
		s.RemoveVolume(volumes[0])

		lost := false
		for ref, state := range s.StateObjects(s.Fixture.Persistent, env) {
			lost = lost || state == "lost"
			_ = ref
		}
		if !lost {
			t.Fatalf("inspect shows no lost object for %s: %v", s.Fixture.Persistent, s.StateObjects(s.Fixture.Persistent, env))
		}
		live := s.Run("check", "--live", "--env", env, "--output", "json")
		if live.Exit != 3 || !strings.Contains(live.Stdout, "ownership.lost_object") {
			t.Fatalf("check --live: %s, want exit 3 with ownership.lost_object", live)
		}
		if r := s.Run("up", "env/"+env, "--env", env, "--timeout", "10m"); r.Exit != 10 {
			t.Fatalf("up over a lost volume: %s, want exit 10", r)
		}
		if contains(s.Inventory(project).Volumes, volumes[0]) {
			t.Fatal("up recreated the lost volume empty")
		}
		s.Must("destroy", "env/"+env, "--env", env, "--timeout", "10m")
		s.Up(env)
	})
}

func TestExposureIsOnlyWhatWasDeclaredAndUnsupportedFailsBeforeMutation(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		want := "127.0.0.1:" + itoa(s.Port(0)) + "->"
		published := s.Published(project)
		if len(published) != 1 || !strings.HasPrefix(published[0], want) {
			t.Fatalf("published ports %v, want exactly one starting %s", published, want)
		}

		dir := s.UnsupportedDir()
		before := s.Everything()
		r := s.RunAt(dir, "up", "--env", "local", "--timeout", "2m")
		if r.Exit == 0 {
			t.Fatalf("an unsupported required capability started: %s", r)
		}
		if got := s.Everything(); strings.Join(got, "|") != strings.Join(before, "|") {
			t.Fatalf("the refused up created runtime objects: before %v after %v", before, got)
		}
	})
}

func TestCheckLiveReportsStatesTakesNoLockAndUnreachableDockerIsUnknown(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		state, objects := s.Fingerprint(), s.Inventory(project)

		without := s.Run("check", "--env", env, "--output", "json")
		for _, r := range liveResults(t, without.Stdout) {
			if harness.Str(r, "state") == "pass" {
				t.Fatalf("check without --live ran the live checker: %s", without)
			}
		}
		live := s.Run("check", "--live", "--env", env, "--output", "json")
		if live.Exit != 0 {
			t.Fatalf("check --live on a healthy environment: %s", live)
		}
		results := liveResults(t, live.Stdout)
		if len(results) == 0 {
			t.Fatalf("check --live printed no live result: %s", live.Stdout)
		}
		for _, r := range results {
			if harness.Str(r, "state") != "pass" || harness.Str(r, "observedAt") == "" || harness.At(r, "generation") == nil {
				t.Fatalf("live result %v, want pass with observedAt and generation (freshness)", r)
			}
		}
		if s.Fingerprint() != state || len(s.Inventory(project).Containers) != len(objects.Containers) {
			t.Fatal("check --live wrote state or changed runtime objects: a hidden mutating probe")
		}
		down := s.RunWith([]string{"DOCKER_HOST=unix:///nonexistent/stackpuddle-acceptance.sock"}, "check", "--live", "--env", env, "--output", "json")
		unknown := false
		for _, r := range liveResults(t, down.Stdout) {
			unknown = unknown || harness.Str(r, "state") == "unknown"
			if harness.Str(r, "state") == "pass" {
				t.Fatalf("an unreachable daemon reported pass: %s", down)
			}
		}
		if down.Exit != 5 || !unknown {
			t.Fatalf("check --live with an unreachable daemon: %s, want exit 5 and an unknown live result", down)
		}
	})
}

func TestCorruptedArchiveFailsItsDigestAndThePreviousVerifiedCacheStaysUsable(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		dry := []string{"up", "env/" + env, "--dry-run", "--env", env}
		s.Server.Corrupt.Store(true)
		if r := s.Run(dry...); r.Exit != 7 {
			t.Fatalf("a corrupted download: %s, want exit 7 (digest mismatch)", r)
		}
		// The bundle fetched before the corrupted archive is verified and legitimately cached, so the cache is
		// not expected to be empty: every entry must hold bytes that hash to its own name.
		if bad := unverifiedEntries(t, cachedFiles(t, s), s.Server.CorruptedDigests()); bad != "" {
			t.Fatalf("a corrupted download entered the cache: %s", bad)
		}
		s.Server.Corrupt.Store(false)
		s.Must(dry...)
		entries := cachedFiles(t, s)
		if len(entries) == 0 {
			t.Fatal("a verified fetch left nothing in the cache")
		}
		good := map[string][]byte{}
		for _, e := range entries {
			data, err := os.ReadFile(e)
			if err != nil {
				t.Fatal(err)
			}
			good[e] = data
		}
		s.Server.Corrupt.Store(true)
		s.Must(dry...) // served from the verified cache, the corrupt server is never needed
		for e, data := range good {
			if now, _ := os.ReadFile(e); string(now) != string(data) {
				t.Fatalf("%s changed: the previous verified entry was replaced", e)
			}
		}
		// Now the cached bytes themselves are damaged: the digest check refuses them too.
		for e, data := range good {
			bad := append([]byte(nil), data...)
			bad[len(bad)/2] ^= 0xff
			if err := os.WriteFile(e, bad, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if r := s.Run(dry...); r.Exit != 7 {
			t.Fatalf("a corrupted cache entry with a corrupt source: %s, want exit 7", r)
		}
	})
}

// unverifiedEntries names the first cache entry whose bytes do not hash to the digest in its file name, or hash
// to one of the corrupted digests; "" when every entry is verified. The CLI names an entry after the digest the
// lock pins, so the name alone proves nothing: only the content check catches bytes stored before verification.
func unverifiedEntries(t *testing.T, files, corrupted []string) string {
	t.Helper()
	for _, e := range files {
		data, err := os.ReadFile(e)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		got := "sha256:" + hex.EncodeToString(sum[:])
		if want := "sha256:" + strings.TrimSuffix(filepath.Base(e), ".tar.gz"); got != want {
			return e + " holds " + got + ", not the digest in its name"
		}
		for _, bad := range corrupted {
			if got == bad {
				return e + " holds the corrupted archive " + bad
			}
		}
	}
	return ""
}

func TestUnverifiedEntriesCatchesCorruptBytesStoredUnderThePinnedName(t *testing.T) {
	dir := t.TempDir()
	good := []byte("verified archive")
	bad := []byte("tampered archive")
	name := func(b []byte) string {
		sum := sha256.Sum256(b)
		return filepath.Join(dir, hex.EncodeToString(sum[:])+".tar.gz")
	}
	write := func(path string, b []byte) {
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(name(good), good)
	if got := unverifiedEntries(t, []string{name(good)}, nil); got != "" {
		t.Fatalf("a verified entry was flagged: %s", got)
	}
	// Corrupt bytes under the pinned (good) name: the shape a store-before-verify CLI leaves behind.
	write(name(good), bad)
	if unverifiedEntries(t, []string{name(good)}, nil) == "" {
		t.Fatal("corrupt bytes stored under the pinned name went undetected")
	}
	// Corrupt bytes whose digest the server reported, even under their own name.
	write(name(bad), bad)
	badSum := sha256.Sum256(bad)
	if unverifiedEntries(t, []string{name(bad)}, []string{"sha256:" + hex.EncodeToString(badSum[:])}) == "" {
		t.Fatal("an entry holding a reported corrupted digest went undetected")
	}
}

func cachedFiles(t *testing.T, s *harness.Stack) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(s.Cache, "stackpuddle", "artifacts", "sha256", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRunCreatesADistinctInvocationAndDryRunRunsNothing(t *testing.T) {
	rows(t, func(t *testing.T, s *harness.Stack) {
		env := s.Fixture.Env(0).Name
		project := seeded(t, s, env)
		one := s.Must("run", s.Fixture.QueryJob, "--env", env, "--output", "json")
		two := s.Must("run", s.Fixture.QueryJob, "--env", env, "--output", "json")
		if one.Stdout == two.Stdout {
			t.Fatalf("two runs printed the same document, want two invocation IDs:\n%s", one.Stdout)
		}
		state, objects := s.Fingerprint(), s.Inventory(project)
		s.Must("run", s.Fixture.QueryJob, "--env", env, "--dry-run")
		if s.Fingerprint() != state || len(s.Inventory(project).Containers) != len(objects.Containers) {
			t.Fatal("run --dry-run recorded an operation or created an object")
		}
	})
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

// liveResults returns the results of the live checker in a check document.
func liveResults(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var out []map[string]any
	results, _ := harness.Decode(t, stdout)["results"].([]any)
	for _, r := range results {
		if m, ok := r.(map[string]any); ok && harness.Str(m, "checker") == "live" {
			out = append(out, m)
		}
	}
	return out
}
