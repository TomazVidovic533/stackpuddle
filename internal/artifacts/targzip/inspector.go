package targzip

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// Inspect validates an archive without extracting or trusting its contents.
func Inspect(kind envelope.Kind, source io.Reader, limits envelope.Limits) error {
	layout, err := envelope.NewLayout(kind)
	if err != nil {
		return err
	}
	maxTarBytes, maxCompressedBytes, err := limits.Bounds()
	if err != nil {
		return err
	}
	tarData, err := readGzip(source, maxTarBytes, maxCompressedBytes)
	if err != nil {
		return err
	}
	return inspectTar(layout, kind, tarData, limits)
}
func inspectTar(layout envelope.Layout, kind envelope.Kind, data []byte, limits envelope.Limits) error {
	descriptor := layout.Descriptor()
	if len(data) < tarEndMarkerSize || len(data)%tarBlockSize != 0 || !bytes.Equal(data[len(data)-tarEndMarkerSize:], make([]byte, tarEndMarkerSize)) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EndMarkers, "", "tar end markers are missing or malformed")
	}
	input := bytes.NewReader(data)
	reader := tar.NewReader(input)
	seen := make(map[string]bool)
	previousName := ""
	var totalBytes int64
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			if count == 0 {
				return envelope.Reject(envelope.InvalidEnvelope, envelope.EmptyArchive, "", "archive is empty")
			}
			if input.Len() != 0 {
				return envelope.Reject(envelope.InvalidEnvelope, envelope.TrailingTar, "", "trailing tar bytes")
			}
			return nil
		}
		if err != nil {
			return &envelope.Error{Category: envelope.InvalidEnvelope, Code: envelope.TarHeader, Message: fmt.Sprintf("tar entry: %v", err), Cause: err}
		}
		// Next skips the preceding entry's padding before reading this header.
		headerOffset := len(data) - input.Len() - tarBlockSize
		if headerOffset < 0 || headerOffset+tarBlockSize > len(data) {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.TarName, "", "noncanonical tar name")
		}
		if err := validateRawHeader(data[headerOffset:headerOffset+tarBlockSize], header.Name); err != nil {
			return err
		}
		if count >= limits.Entries {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryLimit, "", "too many archive entries")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if err := envelope.ValidatePath(name); err != nil {
			return err
		}
		if len(name) > limits.PathBytes || strings.Count(name, "/")+1 > limits.PathDepth {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.PathLimit, name, "archive path exceeds limit: %q", name)
		}
		if count == 0 {
			if name != descriptor || header.Typeflag != tar.TypeReg {
				return envelope.Reject(envelope.InvalidEnvelope, envelope.FirstEntry, name, "first entry must be regular %s", descriptor)
			}
		} else if count > 1 && name <= previousName {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryOrder, name, "archive entries are duplicated or unordered")
		}
		if !layout.Allows(name) {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.OutsideLayout, name, "entry outside %s archive layout: %q", kind, name)
		}
		if err := inspectHeader(header, name == descriptor); err != nil {
			return err
		}
		if name == "content" && header.Typeflag != tar.TypeDir {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.ContentType, name, "content must be a directory")
		}
		if _, exists := seen[name]; exists {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.DuplicateEntry, name, "duplicate archive entry %q", name)
		}
		if err := validateParentDirectories(name, seen); err != nil {
			return err
		}
		seen[name] = header.Typeflag == tar.TypeDir
		if header.Size < 0 || header.Size > limits.FileBytes || (name == descriptor && header.Size > limits.DescriptorBytes) {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.FileLimit, name, "entry %q exceeds size limit", name)
		}
		totalBytes += header.Size
		if totalBytes > limits.TotalBytes {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.TotalLimit, "", "archive exceeds total size limit")
		}
		if err := validatePadding(data, headerOffset, header.Size, name); err != nil {
			return err
		}
		if _, err := io.CopyN(io.Discard, reader, header.Size); err != nil {
			return &envelope.Error{Category: envelope.InvalidEnvelope, Code: envelope.EntryContent, Entry: name, Message: fmt.Sprintf("entry %q content: %v", name, err), Cause: err}
		}
		previousName = name
	}
}
