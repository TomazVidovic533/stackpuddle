//go:build acceptance

package packaged_test

import (
	"strings"
	"syscall"
	"testing"
)

// slowStack is the groups project with a prerequisite job that sleeps long enough to signal mid-operation.
func slowStack(t *testing.T) *stack {
	files := dirFiles(t, "testdata/groups")
	files["stackpuddle.yaml"] = strings.Replace(files["stackpuddle.yaml"], `[sleep, "{{SLOW}}"]`, `[sleep, "8"]`, 1)
	return newStack(t, files)
}

func startSlowUp(t *testing.T, s *stack, group bool) (*proc, string) {
	p := s.start(group, "up", "--timeout", "3m")
	id := p.waitStderr(t, "accepted ")
	s.waitRunning("slow")
	return p, id
}

func TestControls_SigintAndSighupLeaveTheOperationInterruptedAndTheNextUpContinuesIt(t *testing.T) {
	for name, tc := range map[string]struct {
		sig   syscall.Signal
		group bool
	}{"SIGINT to the process group": {syscall.SIGINT, true}, "SIGHUP": {syscall.SIGHUP, false}} {
		t.Run(name, func(t *testing.T) {
			s := slowStack(t)
			p, id := startSlowUp(t, s, tc.group)
			target := p.cmd.Process.Pid
			if tc.group {
				target = -target
			}
			if err := syscall.Kill(target, tc.sig); err != nil {
				t.Fatal(err)
			}
			if r := p.wait(t); r.exit != 8 {
				t.Fatalf("exit %d, want 8 (interrupted)\n%s%s", r.exit, r.stdout, r.stderr)
			}
			if got := str(s.json("inspect", "operation/"+id), "status"); got != "interrupted" {
				t.Fatalf("the operation is %q, want interrupted", got)
			}
			next := s.json("up", "--timeout", "3m")
			if str(next, "operation", "continues") != id || str(next, "operation", "id") == id {
				t.Fatalf("the next up = %v, want a new operation naming %s", next["operation"], id)
			}
		})
	}
}

func TestControls_CancelFromASecondProcessStopsAtAStepBoundaryAndRerunsNothing(t *testing.T) {
	s := slowStack(t)
	p, id := startSlowUp(t, s, false)
	s.mustRun("cancel", id)
	if r := p.wait(t); r.exit != 9 {
		t.Fatalf("exit %d, want 9 (cancelled)\n%s%s", r.exit, r.stdout, r.stderr)
	}
	old := s.json("inspect", "operation/"+id)
	if str(old, "status") != "cancelled" || at(old, "continuedBy") != nil {
		t.Fatalf("operation = %v, want cancelled and not continued", old)
	}
	if steps, _ := at(old, "steps").([]any); len(steps) == 0 {
		t.Fatal("the cancelled operation lost its partial outcomes")
	}
	next := s.json("up", "--timeout", "3m")
	if at(next, "operation", "continues") != nil {
		t.Fatalf("the next up continued a cancelled operation: %v", next["operation"])
	}
}
