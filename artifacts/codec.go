// Package artifacts builds and inspects deterministic tar+gzip archives.
// Inspection checks structure, limits and entry names, not descriptor schemas or publisher
// identity.
package artifacts

import (
	"io"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
)

// Build writes an archive and returns its compressed-byte SHA-256 digest.
// It uses default limits and reads every file through a handle rooted at sourceDir, so a
// symlink swapped in after the walk cannot reach outside it; a file replaced or resized after
// the walk fails with SourceChanged.
func Build(kind Kind, sourceDir string, dst io.Writer) (string, error) {
	return targzip.Build(envelope.Kind(kind), sourceDir, dst)
}

// Inspect checks archive structure, limits and entry names without extracting files. It reads
// the source once as a stream, so memory grows with the entry count, not the archive size.
// It rejects invalid kinds and limits before reading, and names unsafe to extract (case
// collisions, Windows device names, NTFS stream syntax, control characters) with their own codes.
func Inspect(kind Kind, source io.Reader, limits Limits) error {
	return targzip.Inspect(envelope.Kind(kind), source, envelope.Limits(limits))
}

// Digest returns the input's "sha256:"-prefixed hash without validating it.
func Digest(source io.Reader) (string, error) {
	return identity.Digest(source)
}
