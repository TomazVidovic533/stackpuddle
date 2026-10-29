package targzip

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// Source is an archive source walked through a handle rooted at its directory: the plan that
// Write executes. Close releases the handle; a Source is not safe for concurrent use.
type Source struct {
	root    *os.Root
	layout  envelope.Layout
	entries []sourceEntry
}

type sourceEntry struct {
	name string
	info fs.FileInfo
}

// Collect walks sourceDir under the default limits and returns the entries in archive order.
// It rejects anything Build would not write, before writing anything.
func Collect(kind envelope.Kind, sourceDir string) (Source, error) {
	layout, err := envelope.NewLayout(kind)
	if err != nil {
		return Source{}, err
	}
	walked, err := os.Lstat(sourceDir)
	if err != nil {
		return Source{}, err
	}
	if !walked.IsDir() {
		return Source{}, envelope.Reject(envelope.InvalidArgument, envelope.SourceNotDirectory, "", "archive source is not a directory")
	}
	root, err := os.OpenRoot(sourceDir)
	if err != nil {
		return Source{}, err
	}
	walker := sourceWalker{kind: kind, sourceDir: sourceDir, layout: layout, limits: envelope.DefaultLimits(), folded: envelope.NewFoldedNames()}
	entries, err := walker.collect(root, walked)
	if err != nil {
		if closeErr := root.Close(); closeErr != nil {
			return Source{}, errors.Join(err, closeErr)
		}
		return Source{}, err
	}
	return Source{root: root, layout: layout, entries: entries}, nil
}

// Close releases the source directory handle.
func (source Source) Close() error {
	return source.root.Close()
}

// collect walks the opened root after confirming it is the directory Collect checked with Lstat,
// so a directory swapped in between the two calls is refused.
func (walker *sourceWalker) collect(root *os.Root, walked fs.FileInfo) ([]sourceEntry, error) {
	opened, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(walked, opened) {
		return nil, envelope.Reject(envelope.InvalidArgument, envelope.SourceChanged, "", "archive source directory changed while it was opened")
	}
	if err := fs.WalkDir(root.FS(), ".", walker.visit); err != nil {
		return nil, err
	}
	descriptor := walker.layout.Descriptor()
	entries := walker.entries
	slices.SortFunc(entries, func(a, b sourceEntry) int {
		switch {
		case a.name == b.name:
			return 0
		case a.name == descriptor:
			return -1
		case b.name == descriptor:
			return 1
		default:
			return strings.Compare(a.name, b.name)
		}
	})
	if len(entries) == 0 || entries[0].name != descriptor {
		return nil, envelope.Reject(envelope.InvalidArgument, envelope.MissingDescriptor, "", "missing %s", descriptor)
	}
	return entries, nil
}

// sourceWalker applies the archive rules to each walked path under limits read once per build.
type sourceWalker struct {
	kind       envelope.Kind
	sourceDir  string
	layout     envelope.Layout
	limits     envelope.Limits
	folded     envelope.FoldedNames
	entries    []sourceEntry
	totalBytes int64
}

func (walker *sourceWalker) visit(name string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return changedOrRaw(name, walkErr)
	}
	if name == "." {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return changedOrRaw(name, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		path := filepath.Join(walker.sourceDir, filepath.FromSlash(name))
		return envelope.Reject(envelope.InvalidArgument, envelope.SourceEntryType, path, "unsupported source entry %q", path)
	}
	if err := validateName(name, walker.folded); err != nil {
		return asSourceFailure(err)
	}
	return walker.add(name, info)
}

func (walker *sourceWalker) add(name string, info fs.FileInfo) error {
	descriptor := walker.layout.Descriptor()
	if len(name) > walker.limits.PathBytes || strings.Count(name, "/")+1 > walker.limits.PathDepth {
		return envelope.Reject(envelope.InvalidArgument, envelope.PathLimit, name, "source path exceeds archive limit: %q", name)
	}
	if !walker.layout.Allows(name) {
		return envelope.Reject(envelope.InvalidArgument, envelope.OutsideLayout, name, "entry %q outside %s archive layout", name, walker.kind)
	}
	if name == descriptor && !info.Mode().IsRegular() {
		return envelope.Reject(envelope.InvalidArgument, envelope.DescriptorType, name, "descriptor is not a regular file")
	}
	if name == "content" && !info.IsDir() {
		return envelope.Reject(envelope.InvalidArgument, envelope.ContentType, name, "content must be a directory")
	}
	if len(walker.entries) >= walker.limits.Entries {
		return envelope.Reject(envelope.InvalidArgument, envelope.EntryLimit, "", "too many source entries")
	}
	if info.Mode().IsRegular() {
		if info.Size() > walker.limits.FileBytes || (name == descriptor && info.Size() > walker.limits.DescriptorBytes) {
			return envelope.Reject(envelope.InvalidArgument, envelope.FileLimit, name, "source entry %q exceeds archive size limit", name)
		}
		walker.totalBytes += info.Size()
		if walker.totalBytes > walker.limits.TotalBytes {
			return envelope.Reject(envelope.InvalidArgument, envelope.TotalLimit, "", "source exceeds total archive size limit")
		}
	}
	walker.entries = append(walker.entries, sourceEntry{name: name, info: info})
	return nil
}

// A name the archive rules reject is the caller's argument here, not received archive bytes.
func asSourceFailure(err error) error {
	failure, ok := errors.AsType[*envelope.Error](err)
	if !ok {
		return err
	}
	sourceFailure := *failure
	sourceFailure.Category = envelope.InvalidArgument
	return &sourceFailure
}
