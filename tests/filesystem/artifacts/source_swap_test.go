package artifacts_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

// The testkit's compose.yaml holds "services: {}\n"; the replacement has the same size, so only
// the file identity check can tell them apart.
const sameSizeContent = "services: []\n"

func TestWriteRefusesFileSwappedForSymlinkAfterCollect(t *testing.T) {
	root := artifacts.ModuleSource(t)
	if err := os.WriteFile(filepath.Join(root, "content", "other.yaml"), []byte(sameSizeContent), 0644); err != nil {
		t.Fatal(err)
	}
	source := collect(t, root)
	walked := filepath.Join(root, "content", "compose.yaml")
	if err := os.Remove(walked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.yaml", walked); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := source.Write(&archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceChanged)
}

func TestWriteRefusesSymlinkSwappedInThatEscapesTheSource(t *testing.T) {
	root := artifacts.ModuleSource(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte(sameSizeContent), 0644); err != nil {
		t.Fatal(err)
	}
	source := collect(t, root)
	walked := filepath.Join(root, "content", "compose.yaml")
	if err := os.Remove(walked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, walked); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := source.Write(&archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceChanged)
}

func TestWriteRefusesDirectorySwappedForEscapingSymlinkAfterCollect(t *testing.T) {
	root := artifacts.ModuleSource(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "compose.yaml"), []byte(sameSizeContent), 0644); err != nil {
		t.Fatal(err)
	}
	source := collect(t, root)
	walked := filepath.Join(root, "content")
	if err := os.RemoveAll(walked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, walked); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := source.Write(&archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceChanged)
	if _, ok := errors.AsType[*fs.PathError](err); !ok {
		t.Fatalf("source_changed for a swapped directory lost its cause: %v", err)
	}
}

func TestWriteRefusesFileDeletedAfterCollect(t *testing.T) {
	root := artifacts.ModuleSource(t)
	source := collect(t, root)
	if err := os.Remove(filepath.Join(root, "content", "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := source.Write(&archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceChanged)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("source_changed for a deleted file lost its cause: %v", err)
	}
}

func TestWriteRefusesFileResizedAfterCollect(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{"grown", "services:\n  db: {}\n"},
		{"shrunk", "{}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := artifacts.ModuleSource(t)
			source := collect(t, root)
			if err := os.WriteFile(filepath.Join(root, "content", "compose.yaml"), []byte(test.content), 0644); err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			_, err := source.Write(&archive)
			artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceChanged)
		})
	}
}

func collect(t *testing.T, root string) targzip.Source {
	t.Helper()
	source, err := targzip.Collect(envelope.Module, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := source.Close(); err != nil {
			t.Error(err)
		}
	})
	return source
}
