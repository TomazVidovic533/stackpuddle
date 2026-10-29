package artifacts

import "github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"

// Kind identifies a module or bundle archive.
type Kind string

const (
	Module Kind = "module"
	Bundle Kind = "bundle"
)

// Limits caps archive size and entries during Inspect. All fields must be positive.
// Content sizes are uncompressed; paths are limited by byte length and depth.
// Build uses DefaultLimits.
type Limits struct {
	DescriptorBytes int64
	FileBytes       int64
	TotalBytes      int64
	Entries         int
	PathBytes       int
	PathDepth       int
}

// DefaultLimits returns the default archive limits. Each call returns a new value, so changing
// one never changes the defaults.
func DefaultLimits() Limits {
	return Limits(envelope.DefaultLimits())
}

// Category distinguishes invalid arguments from invalid archives.
type Category = envelope.Category

const (
	InvalidArgument = envelope.InvalidArgument
	InvalidEnvelope = envelope.InvalidEnvelope
)

// Code identifies the rule that failed.
type Code = envelope.Code

const (
	CaseCollision      = envelope.CaseCollision
	CompressedLimit    = envelope.CompressedLimit
	ContentType        = envelope.ContentType
	ControlCharacter   = envelope.ControlCharacter
	DescriptorType     = envelope.DescriptorType
	DirectoryEntry     = envelope.DirectoryEntry
	DuplicateEntry     = envelope.DuplicateEntry
	EmptyArchive       = envelope.EmptyArchive
	EndMarkers         = envelope.EndMarkers
	EntryCollision     = envelope.EntryCollision
	EntryContent       = envelope.EntryContent
	EntryLimit         = envelope.EntryLimit
	EntryOrder         = envelope.EntryOrder
	EntryType          = envelope.EntryType
	ExpandedLimit      = envelope.ExpandedLimit
	FileLimit          = envelope.FileLimit
	FirstEntry         = envelope.FirstEntry
	GzipBody           = envelope.GzipBody
	GzipHeader         = envelope.GzipHeader
	LimitsOverflow     = envelope.LimitsOverflow
	Metadata           = envelope.Metadata
	MissingDescriptor  = envelope.MissingDescriptor
	NonpositiveLimits  = envelope.NonpositiveLimits
	OutsideLayout      = envelope.OutsideLayout
	Padding            = envelope.Padding
	PathLimit          = envelope.PathLimit
	RegularEntry       = envelope.RegularEntry
	ReservedBytes      = envelope.ReservedBytes
	ReservedName       = envelope.ReservedName
	SourceChanged      = envelope.SourceChanged
	SourceEntryType    = envelope.SourceEntryType
	SourceNotDirectory = envelope.SourceNotDirectory
	StreamName         = envelope.StreamName
	TarHeader          = envelope.TarHeader
	TarName            = envelope.TarName
	TarType            = envelope.TarType
	TotalLimit         = envelope.TotalLimit
	TrailingCompressed = envelope.TrailingCompressed
	TrailingTar        = envelope.TrailingTar
	UnknownKind        = envelope.UnknownKind
	UnsafePath         = envelope.UnsafePath
)

// Error describes why an archive or argument was rejected. Use errors.AsType to inspect it.
// Entry is the rejected name escaped as a Go string literal without its quotes, so control
// bytes print as escapes such as \x1b. I/O errors may be returned directly; use errors.Is to
// check their cause.
type Error = envelope.Error
