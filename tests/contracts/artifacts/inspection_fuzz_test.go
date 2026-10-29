package artifacts_test

import (
	"bytes"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/testkit/artifacts"
)

func FuzzInspect(f *testing.F) {
	f.Add([]byte{})
	f.Add(artifacts.Archive(f, artifacts.Entry{Name: "module.yaml", Data: "module"}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128<<10 {
			t.Skip()
		}
		limits := envelope.Limits{DescriptorBytes: 1024, FileBytes: 4096, TotalBytes: 8192, Entries: 16, PathBytes: 240, PathDepth: 8}
		_ = targzip.Inspect(envelope.Module, bytes.NewReader(data), limits)
	})
}
