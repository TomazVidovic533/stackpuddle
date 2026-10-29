package artifacts_test

import (
	"testing"

	api "github.com/TomazVidovic533/stackpuddle/artifacts"
)

// The public constants are aliases, so go doc and api.txt list their names but not their
// values. Callers store and compare these strings; this table pins them, and a duplicated value
// fails to compile as a duplicate map key.
func TestPublicCodesAndCategoriesKeepTheirValues(t *testing.T) {
	for code, want := range map[api.Code]string{
		api.CaseCollision:      "case_collision",
		api.CompressedLimit:    "compressed_limit",
		api.ContentType:        "content_type",
		api.ControlCharacter:   "control_character",
		api.DescriptorType:     "descriptor_type",
		api.DirectoryEntry:     "directory_entry",
		api.DuplicateEntry:     "duplicate_entry",
		api.EmptyArchive:       "empty_archive",
		api.EndMarkers:         "end_markers",
		api.EntryCollision:     "entry_collision",
		api.EntryContent:       "entry_content",
		api.EntryLimit:         "entry_limit",
		api.EntryOrder:         "entry_order",
		api.EntryType:          "entry_type",
		api.ExpandedLimit:      "expanded_limit",
		api.FileLimit:          "file_limit",
		api.FirstEntry:         "first_entry",
		api.GzipBody:           "gzip_body",
		api.GzipHeader:         "gzip_header",
		api.LimitsOverflow:     "limits_overflow",
		api.Metadata:           "metadata",
		api.MissingDescriptor:  "missing_descriptor",
		api.NonpositiveLimits:  "nonpositive_limits",
		api.OutsideLayout:      "outside_layout",
		api.Padding:            "padding",
		api.PathLimit:          "path_limit",
		api.RegularEntry:       "regular_entry",
		api.ReservedBytes:      "reserved_bytes",
		api.ReservedName:       "reserved_name",
		api.SourceChanged:      "source_changed",
		api.SourceEntryType:    "source_entry_type",
		api.SourceNotDirectory: "source_not_directory",
		api.StreamName:         "stream_name",
		api.TarHeader:          "tar_header",
		api.TarName:            "tar_name",
		api.TarType:            "tar_type",
		api.TotalLimit:         "total_limit",
		api.TrailingCompressed: "trailing_compressed",
		api.TrailingTar:        "trailing_tar",
		api.UnknownKind:        "unknown_kind",
		api.UnsafePath:         "unsafe_path",
	} {
		if string(code) != want {
			t.Errorf("code %q, want %q", code, want)
		}
	}
	for category, want := range map[api.Category]string{
		api.InvalidArgument: "invalid_argument",
		api.InvalidEnvelope: "invalid_envelope",
	} {
		if string(category) != want {
			t.Errorf("category %q, want %q", category, want)
		}
	}
}
