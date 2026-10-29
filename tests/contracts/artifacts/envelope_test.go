package artifacts_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

func TestInspectAcceptsBuiltModuleAndBundle(t *testing.T) {
	var module bytes.Buffer
	if _, err := targzip.Build(envelope.Module, artifacts.ModuleSource(t), &module); err != nil {
		t.Fatal(err)
	}
	if err := targzip.Inspect(envelope.Module, bytes.NewReader(module.Bytes()), envelope.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	bundle := artifacts.Archive(t, artifacts.Entry{Name: "bundle.yaml", Data: "modules: {}\n"})
	if err := targzip.Inspect(envelope.Bundle, bytes.NewReader(bundle), envelope.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsTamperedSecondFilePadding(t *testing.T) {
	root := artifacts.ModuleSource(t)
	const secondContent = "more content\n"
	if err := os.WriteFile(filepath.Join(root, "content", "second.yaml"), []byte(secondContent), 0644); err != nil {
		t.Fatal(err)
	}
	var built bytes.Buffer
	if _, err := targzip.Build(envelope.Module, root, &built); err != nil {
		t.Fatal(err)
	}
	tarBytes := artifacts.Decompress(t, built.Bytes())
	contentStart := bytes.Index(tarBytes, []byte(secondContent))
	if contentStart < 0 {
		t.Fatal("second file content not found in tar")
	}
	tarBytes[contentStart+len(secondContent)] = 1
	err := targzip.Inspect(envelope.Module, bytes.NewReader(artifacts.Gzip(t, tarBytes)), envelope.DefaultLimits())
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.Padding)
}

func TestInspectRejectsHiddenBytesAfterTarNameNUL(t *testing.T) {
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"}, artifacts.Entry{Name: "content/a", Data: "a"})
	tarBytes := artifacts.Decompress(t, archive)
	const secondHeader = 1024
	tarBytes[secondHeader+len("content/a")+1] = 'b'
	artifacts.RepairChecksum(t, tarBytes[secondHeader:secondHeader+512])
	err := targzip.Inspect(envelope.Module, bytes.NewReader(artifacts.Gzip(t, tarBytes)), envelope.DefaultLimits())
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.TarName)
}

func TestInspectRejectsNULRegularTypeflag(t *testing.T) {
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"})
	tarBytes := artifacts.Decompress(t, archive)
	tarBytes[156] = 0
	artifacts.RepairChecksum(t, tarBytes[:512])
	err := targzip.Inspect(envelope.Module, bytes.NewReader(artifacts.Gzip(t, tarBytes)), envelope.DefaultLimits())
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.TarType)
}

func TestInspectRejectsNonzeroUSTARReservedTail(t *testing.T) {
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"})
	tarBytes := artifacts.Decompress(t, archive)
	tarBytes[500] = 1
	artifacts.RepairChecksum(t, tarBytes[:512])
	err := targzip.Inspect(envelope.Module, bytes.NewReader(artifacts.Gzip(t, tarBytes)), envelope.DefaultLimits())
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.ReservedBytes)
}

func TestInspectBoundsCompressedInputWithEmptyDeflateBlocks(t *testing.T) {
	const blocks = 20_000
	stream := []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 2, 255}
	stream = append(stream, bytes.Repeat([]byte{0, 0, 0, 255, 255}, blocks)...)
	stream = append(stream, 1, 0, 0, 255, 255)
	stream = append(stream, make([]byte, 8)...)
	limits := envelope.Limits{DescriptorBytes: 1, FileBytes: 1, TotalBytes: 1, Entries: 1, PathBytes: 240, PathDepth: 32}
	err := targzip.Inspect(envelope.Module, bytes.NewReader(stream), limits)
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.CompressedLimit)
}

func TestInspectRejectsOverflowingDerivedLimits(t *testing.T) {
	const maxInt64 = int64(^uint64(0) >> 1)
	for name, limits := range map[string]envelope.Limits{
		"total bytes": {DescriptorBytes: 1, FileBytes: 1, TotalBytes: maxInt64, Entries: 1, PathBytes: 1, PathDepth: 1},
		"entry count": {DescriptorBytes: 1, FileBytes: 1, TotalBytes: 1, Entries: int(^uint(0) >> 1), PathBytes: 1, PathDepth: 1},
	} {
		t.Run(name, func(t *testing.T) {
			err := targzip.Inspect(envelope.Module, bytes.NewReader(nil), limits)
			artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.LimitsOverflow)
		})
	}
}

func TestInspectRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []artifacts.Entry
		code    envelope.Code
	}{
		{"wrong first entry", []artifacts.Entry{{Name: "content/a", Data: "a"}, {Name: "module.yaml", Data: "m"}}, envelope.FirstEntry},
		{"duplicate", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/a", Data: "a"}, {Name: "content/a", Data: "a"}}, envelope.EntryOrder},
		{"path traversal", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "../escape", Data: "a"}}, envelope.UnsafePath},
		{"content file before child", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content", Data: "a"}, {Name: "content/a", Data: "a"}}, envelope.ContentType},
		{"nested file directory collision", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/", Kind: tar.TypeDir}, {Name: "content/a", Data: "a"}, {Name: "content/a/b", Data: "b"}}, envelope.EntryCollision},
		{"content is a file", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content", Data: "a"}}, envelope.ContentType},
		{"link", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/link", Kind: tar.TypeSymlink}}, envelope.EntryType},
		{"hardlink", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/hardlink", Kind: tar.TypeLink}}, envelope.EntryType},
		{"fifo", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/fifo", Kind: tar.TypeFifo}}, envelope.EntryType},
		{"device", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/device", Kind: tar.TypeChar}}, envelope.EntryType},
		{"unknown entry type", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content/unknown", Kind: 'Z'}}, envelope.EntryType},
		{"absolute path", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "/absolute", Data: "a"}}, envelope.UnsafePath},
		{"backslash path", []artifacts.Entry{{Name: "module.yaml", Data: "m"}, {Name: "content\\file", Data: "a"}}, envelope.UnsafePath},
		{"extra bundle entry", []artifacts.Entry{{Name: "bundle.yaml", Data: "b"}, {Name: "content/a", Data: "a"}}, envelope.OutsideLayout},
		{"noncanonical owner", []artifacts.Entry{{Name: "module.yaml", Data: "m", UID: 1000}}, envelope.Metadata},
		{"noncanonical file mode", []artifacts.Entry{{Name: "module.yaml", Data: "m", Mode: 0600}}, envelope.RegularEntry},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind := envelope.Module
			if test.name == "extra bundle entry" {
				kind = envelope.Bundle
			}
			archive := artifacts.Archive(t, test.entries...)
			err := targzip.Inspect(kind, bytes.NewReader(archive), envelope.DefaultLimits())
			artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, test.code)
		})
	}
}

func TestInspectRejectsMalformedOrOversizedArchives(t *testing.T) {
	valid := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"})
	second := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"})
	withContent := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"}, artifacts.Entry{Name: "content/a", Data: "content"})
	longNestedPath := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"}, artifacts.Entry{Name: "content/long-filename", Data: "x"})
	optionalEmptyName := append([]byte{}, valid[:10]...)
	optionalEmptyName[3] |= 0x08
	optionalEmptyName = append(optionalEmptyName, 0)
	optionalEmptyName = append(optionalEmptyName, valid[10:]...)
	wrongCompressionHint := bytes.Clone(valid)
	wrongCompressionHint[8] = 0
	invalidTar := artifacts.Gzip(t, []byte("not a tar archive"))
	tarBytes := artifacts.Decompress(t, valid)
	shortTarTrailer := artifacts.Gzip(t, tarBytes[:len(tarBytes)-512])
	badPadding := bytes.Clone(tarBytes)
	badPadding[512+len("module")] = 1
	checks := []struct {
		name   string
		bytes  []byte
		limits envelope.Limits
		code   envelope.Code
	}{
		{"truncated gzip", valid[:len(valid)-4], envelope.DefaultLimits(), envelope.GzipBody},
		{"malformed tar", invalidTar, envelope.DefaultLimits(), envelope.EndMarkers},
		{"missing tar end block", shortTarTrailer, envelope.DefaultLimits(), envelope.EndMarkers},
		{"nonzero file padding", artifacts.Gzip(t, badPadding), envelope.DefaultLimits(), envelope.Padding},
		{"trailing bytes", append(bytes.Clone(valid), 1), envelope.DefaultLimits(), envelope.TrailingCompressed},
		{"second gzip member", append(bytes.Clone(valid), second...), envelope.DefaultLimits(), envelope.TrailingCompressed},
		{"optional empty gzip name", optionalEmptyName, envelope.DefaultLimits(), envelope.GzipHeader},
		{"noncanonical gzip compression hint", wrongCompressionHint, envelope.DefaultLimits(), envelope.GzipHeader},
		{"descriptor limit", valid, envelope.Limits{DescriptorBytes: 1, FileBytes: 10, TotalBytes: 10, Entries: 10, PathBytes: 240, PathDepth: 32}, envelope.FileLimit},
		{"file limit", withContent, envelope.Limits{DescriptorBytes: 10, FileBytes: 5, TotalBytes: 20, Entries: 10, PathBytes: 240, PathDepth: 32}, envelope.FileLimit},
		{"total limit", withContent, envelope.Limits{DescriptorBytes: 10, FileBytes: 10, TotalBytes: 10, Entries: 10, PathBytes: 240, PathDepth: 32}, envelope.TotalLimit},
		{"expanded archive limit", withContent, envelope.Limits{DescriptorBytes: 10, FileBytes: 10, TotalBytes: 20, Entries: 1, PathBytes: 240, PathDepth: 32}, envelope.ExpandedLimit},
		{"entry limit", withContent, envelope.Limits{DescriptorBytes: 10, FileBytes: 10, TotalBytes: 2048, Entries: 1, PathBytes: 240, PathDepth: 32}, envelope.EntryLimit},
		{"nested path length limit", longNestedPath, envelope.Limits{DescriptorBytes: 10, FileBytes: 10, TotalBytes: 20, Entries: 10, PathBytes: 15, PathDepth: 32}, envelope.PathLimit},
		{"path depth limit", withContent, envelope.Limits{DescriptorBytes: 10, FileBytes: 10, TotalBytes: 20, Entries: 10, PathBytes: 240, PathDepth: 1}, envelope.PathLimit},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := targzip.Inspect(envelope.Module, bytes.NewReader(check.bytes), check.limits)
			artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, check.code)
		})
	}
}

func TestInspectAcceptsExactResourceBoundaries(t *testing.T) {
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"}, artifacts.Entry{Name: "content/a", Data: "content"})
	limits := envelope.Limits{DescriptorBytes: 6, FileBytes: 7, TotalBytes: 13, Entries: 2, PathBytes: len("module.yaml"), PathDepth: 2}
	if err := targzip.Inspect(envelope.Module, bytes.NewReader(archive), limits); err != nil {
		t.Fatalf("rejected exact resource boundary: %v", err)
	}
}

func TestInspectRejectsDuplicateRegularDescriptors(t *testing.T) {
	for _, test := range []struct {
		kind       envelope.Kind
		descriptor string
	}{
		{envelope.Module, "module.yaml"},
		{envelope.Bundle, "bundle.yaml"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			archive := artifacts.Archive(t,
				artifacts.Entry{Name: test.descriptor, Data: "first"},
				artifacts.Entry{Name: test.descriptor, Data: "second"},
			)
			err := targzip.Inspect(test.kind, bytes.NewReader(archive), envelope.DefaultLimits())
			artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.DuplicateEntry)
			var failure *envelope.Error
			if !errors.As(err, &failure) || failure.Entry != test.descriptor {
				t.Fatalf("expected duplicate entry context %q, got %v", test.descriptor, err)
			}
		})
	}
}
