package envelope

import "fmt"

// Category separates invalid operation inputs from rejected archive bytes.
type Category string

const (
	InvalidArgument Category = "invalid_argument"
	InvalidEnvelope Category = "invalid_envelope"
)

// Code identifies the violated rule independently of diagnostic wording.
type Code string

const (
	CompressedLimit    Code = "compressed_limit"
	ContentType        Code = "content_type"
	DescriptorType     Code = "descriptor_type"
	DirectoryEntry     Code = "directory_entry"
	DuplicateEntry     Code = "duplicate_entry"
	EmptyArchive       Code = "empty_archive"
	EndMarkers         Code = "end_markers"
	EntryCollision     Code = "entry_collision"
	EntryContent       Code = "entry_content"
	EntryLimit         Code = "entry_limit"
	EntryOrder         Code = "entry_order"
	EntryType          Code = "entry_type"
	ExpandedLimit      Code = "expanded_limit"
	FileLimit          Code = "file_limit"
	FirstEntry         Code = "first_entry"
	GzipBody           Code = "gzip_body"
	GzipHeader         Code = "gzip_header"
	LimitsOverflow     Code = "limits_overflow"
	Metadata           Code = "metadata"
	MissingDescriptor  Code = "missing_descriptor"
	NonpositiveLimits  Code = "nonpositive_limits"
	OutsideLayout      Code = "outside_layout"
	Padding            Code = "padding"
	PathLimit          Code = "path_limit"
	RegularEntry       Code = "regular_entry"
	ReservedBytes      Code = "reserved_bytes"
	SourceEntryType    Code = "source_entry_type"
	SourceNotDirectory Code = "source_not_directory"
	TarHeader          Code = "tar_header"
	TarName            Code = "tar_name"
	TarType            Code = "tar_type"
	TotalLimit         Code = "total_limit"
	TrailingCompressed Code = "trailing_compressed"
	TrailingTar        Code = "trailing_tar"
	UnknownKind        Code = "unknown_kind"
	UnsafePath         Code = "unsafe_path"
)

// Error preserves a rejection's meaning, entry context and original diagnostic.
type Error struct {
	Category Category
	Code     Code
	Entry    string
	Message  string
	Cause    error
}

func (err *Error) Error() string { return err.Message }
func (err *Error) Unwrap() error { return err.Cause }

// Reject constructs a classified failure without changing its diagnostic text.
func Reject(category Category, code Code, entry, format string, args ...any) error {
	return &Error{Category: category, Code: code, Entry: entry, Message: fmt.Sprintf(format, args...)}
}
