//go:build acceptance

package packaged_test

import (
	"os"
	"strings"
	"testing"
)

// Each case breaks one compatibility input and expects exit 7 before any Docker or database change: the
// state directory's bytes and the labelled Docker inventory are equal before and after.
func TestCompatibility_EachRejectionExitsSevenAndChangesNothing(t *testing.T) {
	cases := map[string]func(t *testing.T) *stack{
		"unsupported model apiVersion": func(t *testing.T) *stack {
			return newPackaged(t, "postgres", func(f map[string]string) {
				f["stackpuddle.yaml"] = strings.Replace(f["stackpuddle.yaml"], "apiVersion: stackpuddle/v1\nkind: Project", "apiVersion: stackpuddle/v99\nkind: Project", 1)
			})
		},
		"module outside the bundle": func(t *testing.T) *stack { return newPackaged(t, "other", nil) },
		"unadvertised capability": func(t *testing.T) *stack {
			return newStack(t, dirFiles(t, "testdata/compat/capability"))
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			s := build(t)
			before := s.stateHash() + s.names()
			r := s.run("up", "--timeout", "1m")
			if r.exit != 7 {
				t.Fatalf("exit %d, want 7 (compatibility)\nstdout: %s\nstderr: %s", r.exit, r.stdout, r.stderr)
			}
			if after := s.stateHash() + s.names(); after != before {
				t.Fatalf("state or Docker inventory changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestCompatibility_ANewerStateVersionAndANewerStoreVersionExitSeven(t *testing.T) {
	s := newPackaged(t, "postgres", nil)
	s.mustRun("up", "--timeout", "5m")

	states := s.stateFiles("state.json")
	if len(states) != 1 {
		t.Fatalf("state files = %v, want exactly one state.json", states)
	}
	original, err := os.ReadFile(states[0])
	if err != nil {
		t.Fatal(err)
	}
	newer := strings.Replace(string(original), `"stateVersion": 1`, `"stateVersion": 99`, 1)
	if newer == string(original) {
		t.Fatalf("state.json has no `\"stateVersion\": 1` to raise:\n%s", original)
	}
	write(t, states[0], newer)
	before := s.stateHash() + s.names()
	if r := s.run("up", "--timeout", "1m"); r.exit != 7 {
		t.Fatalf("newer stateVersion: exit %d, want 7\n%s%s", r.exit, r.stdout, r.stderr)
	}
	if after := s.stateHash() + s.names(); after != before {
		t.Fatalf("a newer stateVersion changed state or Docker inventory")
	}
	write(t, states[0], string(original))

	stores := s.stateFiles("*.sealed")
	if len(stores) == 0 {
		t.Fatal("no sealed secret store under the state directory")
	}
	sealed, err := os.ReadFile(stores[0])
	if err != nil {
		t.Fatal(err)
	}
	sealed[len("SPSEALED")]++ // the store version byte follows the magic
	if err := os.WriteFile(stores[0], sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	before = s.stateHash() + s.names()
	if r := s.run("up", "--timeout", "1m"); r.exit != 7 {
		t.Fatalf("newer store version: exit %d, want 7\n%s%s", r.exit, r.stdout, r.stderr)
	}
	if after := s.stateHash() + s.names(); after != before {
		t.Fatalf("a newer store version changed state or Docker inventory")
	}
}
