//go:build acceptance

package packaged_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var imageID = regexp.MustCompile(`"imageId": "(sha256:[0-9a-f]{64})"`)

func (s *stack) recordedImageIDs() []string {
	var ids []string
	for _, f := range s.stateFiles("state.json") {
		data, err := os.ReadFile(f)
		if err != nil {
			s.t.Fatal(err)
		}
		for _, m := range imageID.FindAllStringSubmatch(string(data), -1) {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func TestImage_ADeclaredImageAndABuildAreRecordedAndAMovedTagDoesNotUpgradeThem(t *testing.T) {
	s := newStack(t, dirFiles(t, "testdata/build"))
	tag := s.name + "-img:1"
	s.tags = append(s.tags, tag)
	docker(t, "tag", busybox, tag) // pulled by the harness's first use; the tag is this test's own
	s.mustRun("up", "--all", "--timeout", "5m")
	recorded := s.recordedImageIDs()
	if len(recorded) < 2 {
		t.Fatalf("recorded image IDs = %v, want one for the declared image and one for the build", recorded)
	}

	// Move the tag to other bytes: a different image built from the same base.
	other := filepath.Join(t.TempDir(), "Dockerfile")
	write(t, other, "FROM "+busybox+"\nLABEL sp.moved=yes\n")
	docker(t, "build", "-q", "-t", tag, "-f", other, filepath.Dir(other))

	again := s.mustRun("up", "--all", "--timeout", "5m")
	after := s.recordedImageIDs()
	if strings.Join(after, ",") != strings.Join(recorded, ",") {
		t.Fatalf("a repeat up upgraded the recorded artifact: %v -> %v", recorded, after)
	}
	if !regexp.MustCompile(`(?i)differ|drift|moved|changed`).MatchString(again.stdout + again.stderr) {
		t.Fatalf("a repeat up did not report the difference:\n%s%s", again.stdout, again.stderr)
	}
}
