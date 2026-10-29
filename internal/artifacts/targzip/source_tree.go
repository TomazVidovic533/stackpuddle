package targzip

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

type sourceEntry struct {
	name string
	path string
	info os.FileInfo
}

func collectSource(kind envelope.Kind, root string, layout envelope.Layout) ([]sourceEntry, error) {
	descriptor := layout.Descriptor()
	var entries []sourceEntry
	var totalBytes int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			if !info.IsDir() {
				return envelope.Reject(envelope.InvalidArgument, envelope.SourceNotDirectory, "", "archive source is not a directory")
			}
			return nil
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return envelope.Reject(envelope.InvalidArgument, envelope.SourceEntryType, path, "unsupported source entry %q", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if err := envelope.ValidatePath(name); err != nil {
			var failure *envelope.Error
			if errors.As(err, &failure) {
				sourceFailure := *failure
				sourceFailure.Category = envelope.InvalidArgument
				return &sourceFailure
			}
			return err
		}
		if len(name) > envelope.DefaultLimits.PathBytes || strings.Count(name, "/")+1 > envelope.DefaultLimits.PathDepth {
			return envelope.Reject(envelope.InvalidArgument, envelope.PathLimit, name, "source path exceeds archive limit: %q", name)
		}
		if !layout.Allows(name) {
			return envelope.Reject(envelope.InvalidArgument, envelope.OutsideLayout, name, "entry %q outside %s archive layout", name, kind)
		}
		if name == descriptor && !info.Mode().IsRegular() {
			return envelope.Reject(envelope.InvalidArgument, envelope.DescriptorType, name, "descriptor is not a regular file")
		}
		if name == "content" && !info.IsDir() {
			return envelope.Reject(envelope.InvalidArgument, envelope.ContentType, name, "content must be a directory")
		}
		if len(entries) >= envelope.DefaultLimits.Entries {
			return envelope.Reject(envelope.InvalidArgument, envelope.EntryLimit, "", "too many source entries")
		}
		if info.Mode().IsRegular() {
			if info.Size() > envelope.DefaultLimits.FileBytes || (name == descriptor && info.Size() > envelope.DefaultLimits.DescriptorBytes) {
				return envelope.Reject(envelope.InvalidArgument, envelope.FileLimit, name, "source entry %q exceeds archive size limit", name)
			}
			totalBytes += info.Size()
			if totalBytes > envelope.DefaultLimits.TotalBytes {
				return envelope.Reject(envelope.InvalidArgument, envelope.TotalLimit, "", "source exceeds total archive size limit")
			}
		}
		entries = append(entries, sourceEntry{name: name, path: path, info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name == descriptor || entries[j].name == descriptor {
			return entries[i].name == descriptor
		}
		return entries[i].name < entries[j].name
	})
	if len(entries) == 0 || entries[0].name != descriptor {
		return nil, envelope.Reject(envelope.InvalidArgument, envelope.MissingDescriptor, "", "missing %s", descriptor)
	}
	return entries, nil
}
