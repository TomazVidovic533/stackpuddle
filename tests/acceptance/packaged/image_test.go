//go:build acceptance

package packaged_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	docker(t, "pull", busybox)
	docker(t, "tag", busybox, tag) // the tag is this test's own and the only one removed
	s.mustRun("up", "--all", "--timeout", "5m")
	recorded := s.recordedImageIDs()
	if len(recorded) < 2 {
		t.Fatalf("recorded image IDs = %v, want one for the declared image and one for the build", recorded)
	}

	// Move the tag to other bytes: a different image built from the same base.
	other := filepath.Join(t.TempDir(), "Dockerfile")
	write(t, other, "FROM "+busybox+"\nLABEL sp.moved=yes\n")
	docker(t, "build", "-q", "-t", tag, "-f", other, filepath.Dir(other))
	moved := docker(t, "image", "inspect", "--format", "{{.Id}}", tag)
	if slices.Contains(recorded, moved) {
		t.Fatalf("the moved tag has the recorded image ID %s, so the test moved nothing", moved)
	}

	s.mustRun("up", "--all", "--timeout", "5m")
	if after := s.recordedImageIDs(); !slices.Equal(after, recorded) {
		t.Fatalf("a repeat up upgraded the recorded artifact: %v -> %v", recorded, after)
	}
	for _, id := range s.containerImageIDs() {
		if id == moved {
			t.Fatalf("a container runs the moved tag's image %s, want only recorded images %v", moved, recorded)
		}
		if !slices.Contains(recorded, id) {
			t.Fatalf("a container runs image %s, which is not a recorded one (%v)", id, recorded)
		}
	}
}

// containerImageIDs returns the image ID each of this stack's containers was created from.
func (s *stack) containerImageIDs() []string {
	s.t.Helper()
	containers := strings.Fields(docker(s.t, "ps", "-a", "-q", "--filter", "name="+s.name))
	if len(containers) == 0 {
		s.t.Fatalf("no container of %s exists to check", s.name)
	}
	return strings.Fields(docker(s.t, append([]string{"inspect", "--format", "{{.Image}}"}, containers...)...))
}
