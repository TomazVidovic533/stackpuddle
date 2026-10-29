package artifacts_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	api "github.com/TomazVidovic533/stackpuddle/artifacts"
	testkit "github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

func TestPublicBuildAndInspect(t *testing.T) {
	for _, kind := range []api.Kind{api.Module, api.Bundle} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			descriptor := string(kind) + ".yaml"
			if err := os.WriteFile(filepath.Join(root, descriptor), []byte("opaque descriptor\n"), 0644); err != nil {
				t.Fatal(err)
			}
			var first, second bytes.Buffer
			digest, err := api.Build(kind, root, &first)
			if err != nil {
				t.Fatal(err)
			}
			secondDigest, err := api.Build(kind, root, &second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) || digest != secondDigest {
				t.Fatal("same source produced different archives or digests")
			}
			wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(first.Bytes()))
			if digest != wantDigest {
				t.Fatalf("build digest = %q, want %q", digest, wantDigest)
			}
			if err := api.Inspect(kind, bytes.NewReader(first.Bytes()), api.DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			actualDigest, err := api.Digest(bytes.NewReader(first.Bytes()))
			if err != nil || actualDigest != wantDigest {
				t.Fatalf("digest = %q, %v; want %q", actualDigest, err, wantDigest)
			}
		})
	}
}

func TestPublicRejectionDetails(t *testing.T) {
	for _, kind := range []api.Kind{api.Module, api.Bundle} {
		t.Run(string(kind), func(t *testing.T) {
			name := string(kind) + ".yaml"
			data := testkit.Archive(t, testkit.Entry{Name: name}, testkit.Entry{Name: name})
			err := api.Inspect(kind, bytes.NewReader(data), api.DefaultLimits())
			var failure *api.Error
			if !errors.As(err, &failure) {
				t.Fatalf("expected *artifacts.Error, got %v", err)
			}
			if failure.Category != api.InvalidEnvelope || failure.Code != api.DuplicateEntry || failure.Entry != name {
				t.Fatalf("unexpected rejection: %+v", failure)
			}
		})
	}
}

func TestPublicInvalidInputsBeforeIO(t *testing.T) {
	cause := errors.New("unexpected input read")
	for _, tc := range []struct {
		name   string
		kind   api.Kind
		limits api.Limits
		code   api.Code
	}{
		{"unknown kind", api.Kind("unknown"), api.DefaultLimits(), api.UnknownKind},
		{"zero limits", api.Module, api.Limits{}, api.NonpositiveLimits},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := api.Inspect(tc.kind, publicFailingStream{cause}, tc.limits)
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Category != api.InvalidArgument || failure.Code != tc.code {
				t.Fatalf("unexpected rejection: %v", err)
			}
			if errors.Is(err, cause) {
				t.Fatal("read source before rejecting invalid arguments")
			}
		})
	}
}

func TestPublicIOErrors(t *testing.T) {
	cause := errors.New("stream unavailable")
	stream := publicFailingStream{cause}
	if _, err := api.Digest(stream); !errors.Is(err, cause) {
		t.Fatalf("Digest did not preserve the read error: %v", err)
	}
	if err := api.Inspect(api.Module, stream, api.DefaultLimits()); !errors.Is(err, cause) {
		t.Fatalf("Inspect did not preserve the read error: %v", err)
	}
	if _, err := api.Build(api.Module, testkit.ModuleSource(t), stream); !errors.Is(err, cause) {
		t.Fatalf("Build did not preserve the write error: %v", err)
	}
}

func TestPublicCustomLimits(t *testing.T) {
	limits := api.DefaultLimits()
	original := limits
	limits.DescriptorBytes = 1
	if api.DefaultLimits() != original {
		t.Fatal("custom limits changed the defaults")
	}
	data := testkit.Archive(t, testkit.Entry{Name: "module.yaml", Data: "oversized"})
	err := api.Inspect(api.Module, bytes.NewReader(data), limits)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Category != api.InvalidEnvelope || failure.Code != api.FileLimit || failure.Entry != "module.yaml" {
		t.Fatalf("expected descriptor size error, got %v", err)
	}
}

type publicFailingStream struct{ cause error }

func (s publicFailingStream) Read([]byte) (int, error)  { return 0, s.cause }
func (s publicFailingStream) Write([]byte) (int, error) { return 0, s.cause }

var _ io.ReadWriter = publicFailingStream{}
