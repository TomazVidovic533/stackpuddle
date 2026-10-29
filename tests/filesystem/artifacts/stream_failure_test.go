package artifacts_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/targzip"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

type failingReader struct{ cause error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.cause }

type failingWriter struct{ cause error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.cause }
func TestStreamFailuresPreserveOperationalCause(t *testing.T) {
	cause := errors.New("injected stream failure")
	archive := artifacts.Archive(t, artifacts.Entry{Name: "module.yaml", Data: "module"})
	for _, offset := range []int{0, 10, len(archive) - 4, len(archive)} {
		source := io.MultiReader(bytes.NewReader(archive[:offset]), failingReader{cause})
		err := targzip.Inspect(envelope.Module, source, envelope.DefaultLimits())
		if !errors.Is(err, cause) {
			t.Fatalf("offset %d lost cause: %v", offset, err)
		}
		var invalid *envelope.Error
		if errors.As(err, &invalid) {
			t.Fatalf("offset %d classified I/O as invalid archive: %+v", offset, invalid)
		}
	}
	digest, err := identity.Digest(failingReader{cause})
	if digest != "" || !errors.Is(err, cause) {
		t.Fatalf("digest returned %q, %v", digest, err)
	}
	digest, err = targzip.Build(envelope.Module, artifacts.ModuleSource(t), failingWriter{cause})
	if digest != "" || !errors.Is(err, cause) {
		t.Fatalf("build returned %q, %v", digest, err)
	}
}
