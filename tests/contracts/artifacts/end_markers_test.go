package artifacts_test

import (
	"bytes"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

// Go's tar.Reader accepts an archive that ends after one zero block or none. When the last
// content block is itself zero, the final 1024 bytes are zero anyway, so only counting the
// blocks after the last entry catches the missing marker.
func TestInspectRejectsOneEndMarkerAfterZeroContent(t *testing.T) {
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: string(make([]byte, 512))})
	tarBytes := artifacts.Decompress(t, archive)
	oneMarker := tarBytes[:len(tarBytes)-512]
	err := targzip.Inspect(envelope.Module, bytes.NewReader(artifacts.Gzip(t, oneMarker)), envelope.DefaultLimits())
	artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.EndMarkers)
	if err := targzip.Inspect(envelope.Module, bytes.NewReader(archive), envelope.DefaultLimits()); err != nil {
		t.Fatalf("rejected the same archive with both end markers: %v", err)
	}
}
