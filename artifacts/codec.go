// Package artifacts builds and inspects deterministic tar+gzip archives.
// Inspection checks structure, not descriptor schemas or publisher identity.
package artifacts

import (
	"io"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
)

// Build writes an archive and returns its compressed-byte SHA-256 digest.
// It uses default limits and requires a stable source tree.
func Build(kind Kind, sourceDir string, dst io.Writer) (string, error) {
	return targzip.Build(envelope.Kind(kind), sourceDir, dst)
}

// Inspect checks archive structure and limits without extracting files.
// It rejects invalid kinds and limits before reading.
func Inspect(kind Kind, source io.Reader, limits Limits) error {
	return targzip.Inspect(envelope.Kind(kind), source, envelope.Limits(limits))
}

// Digest returns the input's "sha256:"-prefixed hash without validating it.
func Digest(source io.Reader) (string, error) {
	return identity.Digest(source)
}
