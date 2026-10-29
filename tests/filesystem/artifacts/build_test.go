package artifacts_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

func TestBuildProducesIdenticalModuleBytesAndDigest(t *testing.T) {
	first := artifacts.ModuleSource(t)
	second := artifacts.ModuleSource(t)
	if err := os.Chtimes(filepath.Join(second, "module.yaml"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(second, "module.yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	var firstArchive, secondArchive bytes.Buffer
	firstDigest, err := targzip.Build(envelope.Module, first, &firstArchive)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := targzip.Build(envelope.Module, second, &secondArchive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstArchive.Bytes(), secondArchive.Bytes()) || firstDigest != secondDigest {
		t.Fatal("equivalent source trees produced different archives")
	}
}

func TestBuildRejectsSymlink(t *testing.T) {
	root := artifacts.ModuleSource(t)
	if err := os.Symlink("module.yaml", filepath.Join(root, "content", "link")); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := targzip.Build(envelope.Module, root, &archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.SourceEntryType)
}

func TestBuildRejectsContentFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "module.yaml"), []byte("module"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content"), []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := targzip.Build(envelope.Module, root, &archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.ContentType)
}

func TestBuildBundleContainsOnlyDescriptor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bundle.yaml"), []byte("bundle"), 0644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	digest, err := targzip.Build(envelope.Bundle, root, &archive)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := identity.Digest(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if digest != actual {
		t.Fatalf("build digest %q differs from archive digest %q", digest, actual)
	}
	if err := targzip.Inspect(envelope.Bundle, bytes.NewReader(archive.Bytes()), envelope.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAndInspectMultipleUnalignedContentFiles(t *testing.T) {
	root := artifacts.ModuleSource(t)
	if err := os.WriteFile(filepath.Join(root, "content", "second.yaml"), []byte("more content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := targzip.Build(envelope.Module, root, &archive); err != nil {
		t.Fatal(err)
	}
	if err := targzip.Inspect(envelope.Module, bytes.NewReader(archive.Bytes()), envelope.DefaultLimits()); err != nil {
		t.Fatalf("inspecting a built archive: %v", err)
	}
}

func TestBuildOrdersEntriesAndNormalizesModes(t *testing.T) {
	root := artifacts.ModuleSource(t)
	if err := os.WriteFile(filepath.Join(root, "content", "z-tool"), []byte("run"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "a-config"), []byte("config"), 0600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := targzip.Build(envelope.Module, root, &archive); err != nil {
		t.Fatal(err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if header.Name == "content/z-tool" && header.Mode != 0755 {
			t.Fatalf("executable mode = %#o", header.Mode)
		}
		if header.Name == "content/a-config" && header.Mode != 0644 {
			t.Fatalf("ordinary mode = %#o", header.Mode)
		}
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "module.yaml,content/,content/a-config,content/compose.yaml,content/z-tool"; got != want {
		t.Fatalf("archive order = %q, want %q", got, want)
	}
}

func TestBuildRejectsDescriptorAboveInspectionLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "module.yaml"), bytes.Repeat([]byte("x"), int(envelope.DefaultLimits().DescriptorBytes)+1), 0644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	_, err := targzip.Build(envelope.Module, root, &archive)
	artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.FileLimit)
}

func TestInspectAcceptsNearBoundaryIncompressibleBuild(t *testing.T) {
	root := artifacts.ModuleSource(t)
	payload := make([]byte, 2<<20)
	if _, err := rand.New(rand.NewSource(1)).Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "random.bin"), payload, 0644); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := targzip.Build(envelope.Module, root, &archive); err != nil {
		t.Fatal(err)
	}
	limits := envelope.Limits{
		DescriptorBytes: int64(len("Name: postgres\n")),
		FileBytes:       int64(len(payload)),
		TotalBytes:      int64(len(payload) + len("Name: postgres\n") + len("services: {}\n")),
		Entries:         4,
		PathBytes:       240,
		PathDepth:       32,
	}
	if err := targzip.Inspect(envelope.Module, bytes.NewReader(archive.Bytes()), limits); err != nil {
		t.Fatalf("rejected near-boundary incompressible build: %v", err)
	}
}
