package envelope_test

import (
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/tests/testkit/artifacts"
)

func TestLimitsRequireEveryPositiveField(t *testing.T) {
	for _, field := range []string{"descriptor", "file", "total", "entries", "path", "depth"} {
		t.Run(field, func(t *testing.T) {
			limits := envelope.DefaultLimits
			switch field {
			case "descriptor":
				limits.DescriptorBytes = 0
			case "file":
				limits.FileBytes = 0
			case "total":
				limits.TotalBytes = 0
			case "entries":
				limits.Entries = 0
			case "path":
				limits.PathBytes = 0
			case "depth":
				limits.PathDepth = 0
			}
			_, _, err := limits.Bounds()
			artifacts.RequireFailure(t, err, envelope.InvalidArgument, envelope.NonpositiveLimits)
		})
	}
}
func TestBoundsReserveHeadersPaddingAndCompressedExpansion(t *testing.T) {
	limits := envelope.Limits{DescriptorBytes: 1, FileBytes: 1, TotalBytes: 1, Entries: 1, PathBytes: 1, PathDepth: 1}
	tar, compressed, err := limits.Bounds()
	if err != nil {
		t.Fatal(err)
	}
	if tar != 2049 || compressed != 67713 {
		t.Fatalf("unexpected bounds: %d %d", tar, compressed)
	}
}
