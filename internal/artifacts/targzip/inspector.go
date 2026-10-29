package targzip

import (
	"archive/tar"
	"io"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// Inspect validates an archive without extracting or trusting its contents. It reads the
// archive once as a stream, so memory grows with the number of entries, never with their size.
func Inspect(kind envelope.Kind, source io.Reader, limits envelope.Limits) error {
	layout, err := envelope.NewLayout(kind)
	if err != nil {
		return err
	}
	maxTarBytes, maxCompressedBytes, err := limits.Bounds()
	if err != nil {
		return err
	}
	stream, err := openGzip(source, maxTarBytes, maxCompressedBytes)
	if err != nil {
		return err
	}
	tracker := newTarTracker(stream.expanded())
	walk := walkTar(tracker, kind, layout, limits)
	tracker.drain()
	// A failure found early in the stream yields to one found over the whole stream, in the
	// order a fully buffered inspection reported them.
	if err := stream.finish(tracker.readErr, tracker.offset); err != nil {
		return err
	}
	if !tracker.hasEndMarkers() || (walk.complete && walk.endMarkerBytes < tarEndMarkerSize) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EndMarkers, "", "tar end markers are missing or malformed")
	}
	if walk.failure != nil {
		return walk.failure
	}
	if walk.entries == 0 {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EmptyArchive, "", "archive is empty")
	}
	if tracker.offset != walk.tarEnd {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.TrailingTar, "", "trailing tar bytes")
	}
	return nil
}

// tarWalk is what one pass of tar.Reader found: the first entry-level failure in stream order,
// or, when tar.Reader reached its end, the entry count and where the end markers began and ended.
type tarWalk struct {
	failure        error
	complete       bool
	entries        int
	endMarkerBytes int64
	tarEnd         int64
}

// tarEntries holds the per-entry state of one walk: names seen, their case folding, the
// previous name and the running content total.
type tarEntries struct {
	kind         envelope.Kind
	layout       envelope.Layout
	limits       envelope.Limits
	seen         map[string]bool
	folded       envelope.FoldedNames
	previousName string
	totalBytes   int64
}

func walkTar(tracker *tarTracker, kind envelope.Kind, layout envelope.Layout, limits envelope.Limits) tarWalk {
	reader := tar.NewReader(tracker)
	entries := tarEntries{kind: kind, layout: layout, limits: limits, seen: make(map[string]bool), folded: envelope.NewFoldedNames()}
	var entryEnd int64
	for count := 0; ; count++ {
		header, err := reader.Next()
		// Next consumes the previous entry's padding before the next header, so a padding
		// failure precedes anything about this entry.
		if failure := tracker.paddingFailure(); failure != nil {
			return tarWalk{failure: failure}
		}
		if err == io.EOF {
			return tarWalk{complete: true, entries: count, endMarkerBytes: tracker.offset - entryEnd, tarEnd: tracker.offset}
		}
		if err != nil {
			return tarWalk{failure: envelope.RejectCause(envelope.InvalidEnvelope, envelope.TarHeader, "", err, "tar entry: %v", err)}
		}
		if err := entries.accept(tracker.lastBlock(), header, count); err != nil {
			return tarWalk{failure: err}
		}
		name := strings.TrimSuffix(header.Name, "/")
		tracker.expectPadding(name, header.Size)
		entryEnd = tracker.entryEnd()
		if _, err := io.CopyN(io.Discard, reader, header.Size); err != nil {
			return tarWalk{failure: envelope.RejectCause(envelope.InvalidEnvelope, envelope.EntryContent, name, err, "entry %q content: %v", name, err)}
		}
	}
}

// accept checks one entry's raw header and metadata against the layout and limits, in a fixed
// order that callers match on, and records it.
func (entries *tarEntries) accept(rawHeader []byte, header *tar.Header, count int) error {
	descriptor := entries.layout.Descriptor()
	if err := validateRawHeader(rawHeader, header.Name); err != nil {
		return err
	}
	if count >= entries.limits.Entries {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryLimit, "", "too many archive entries")
	}
	name := strings.TrimSuffix(header.Name, "/")
	if err := validateName(name, entries.folded); err != nil {
		return err
	}
	if len(name) > entries.limits.PathBytes || strings.Count(name, "/")+1 > entries.limits.PathDepth {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.PathLimit, name, "archive path exceeds limit: %q", name)
	}
	if count == 0 {
		if name != descriptor || header.Typeflag != tar.TypeReg {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.FirstEntry, name, "first entry must be regular %s", descriptor)
		}
	} else if count > 1 && name <= entries.previousName {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryOrder, name, "archive entries are duplicated or unordered")
	}
	if !entries.layout.Allows(name) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.OutsideLayout, name, "entry outside %s archive layout: %q", entries.kind, name)
	}
	if err := inspectHeader(header, name == descriptor); err != nil {
		return err
	}
	if name == "content" && header.Typeflag != tar.TypeDir {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.ContentType, name, "content must be a directory")
	}
	if _, exists := entries.seen[name]; exists {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.DuplicateEntry, name, "duplicate archive entry %q", name)
	}
	if err := validateParentDirectories(name, entries.seen); err != nil {
		return err
	}
	entries.seen[name] = header.Typeflag == tar.TypeDir
	if header.Size < 0 || header.Size > entries.limits.FileBytes || (name == descriptor && header.Size > entries.limits.DescriptorBytes) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.FileLimit, name, "entry %q exceeds size limit", name)
	}
	entries.totalBytes += header.Size
	if entries.totalBytes > entries.limits.TotalBytes {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.TotalLimit, "", "archive exceeds total size limit")
	}
	entries.previousName = name
	return nil
}

// validateName applies the path rules, then the extraction rules, then case folding.
func validateName(name string, folded envelope.FoldedNames) error {
	if err := envelope.ValidatePath(name); err != nil {
		return err
	}
	if err := envelope.ValidateExtractable(name); err != nil {
		return err
	}
	return folded.Claim(name)
}
