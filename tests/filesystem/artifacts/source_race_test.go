package artifacts_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

// A file that flips between its own inode and an escaping symlink while Build runs fails Build
// with a source_changed envelope error or succeeds; no raw operating-system error comes back.
func TestBuildClassifiesEveryErrorWhileFileFlipsToEscapingSymlink(t *testing.T) {
	const builds = 300
	root := artifacts.ModuleSource(t)
	base := t.TempDir()
	outside := filepath.Join(base, "outside.yaml")
	if err := os.WriteFile(outside, []byte(sameSizeContent), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "content", "compose.yaml")
	original := filepath.Join(base, "original.yaml")
	if err := os.Link(target, original); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	done := make(chan error, 1)
	go func() {
		link, tmp := filepath.Join(base, "link"), filepath.Join(base, "tmp")
		for !stop.Load() {
			for _, step := range []func() error{
				func() error { return os.Symlink(outside, link) },
				func() error { return os.Rename(link, target) },
				func() error { return os.Link(original, tmp) },
				func() error { return os.Rename(tmp, target) },
			} {
				if err := step(); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	for i := 0; i < builds; i++ {
		if _, err := targzip.Build(envelope.Module, root, io.Discard); err != nil {
			if _, ok := errors.AsType[*envelope.Error](err); !ok {
				stop.Store(true)
				<-done
				t.Fatalf("build %d returned a raw error: %v", i, err)
			}
		}
	}
	stop.Store(true)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The content directory flips between itself and a symlink to an outside directory while Build
// walks it: the walk's own errors are classified too, so no raw operating-system error comes back.
func TestBuildClassifiesEveryErrorWhileContentDirFlips(t *testing.T) {
	const builds = 300
	root := artifacts.ModuleSource(t)
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "compose.yaml"), []byte(sameSizeContent), 0644); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(root, "content")
	parked := filepath.Join(base, "parked")
	var stop atomic.Bool
	done := make(chan error, 1)
	go func() {
		for !stop.Load() {
			for _, step := range []func() error{
				func() error { return os.Rename(content, parked) },
				func() error { return os.Symlink(outside, content) },
				func() error { return os.Remove(content) },
				func() error { return os.Rename(parked, content) },
			} {
				if err := step(); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	for i := 0; i < builds; i++ {
		if _, err := targzip.Build(envelope.Module, root, io.Discard); err != nil {
			if _, ok := errors.AsType[*envelope.Error](err); !ok {
				stop.Store(true)
				<-done
				t.Fatalf("build %d returned a raw error: %v", i, err)
			}
		}
	}
	stop.Store(true)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
