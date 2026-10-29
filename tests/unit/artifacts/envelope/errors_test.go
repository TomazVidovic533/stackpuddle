package envelope_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

func TestUnknownKindHasStructuredReason(t *testing.T) {
	_, err := envelope.NewLayout("unknown")
	var failure *envelope.Error
	if !errors.As(fmt.Errorf("build: %w", err), &failure) {
		t.Fatalf("expected structured error, got %v", err)
	}
	if failure.Category != envelope.InvalidArgument || failure.Code != envelope.UnknownKind {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if err.Error() != `unknown archive kind "unknown"` {
		t.Fatalf("changed message: %v", err)
	}
}
func TestFailurePreservesCause(t *testing.T) {
	cause := errors.New("cause")
	err := &envelope.Error{Category: envelope.InvalidEnvelope, Code: envelope.GzipBody, Message: "gzip body: cause", Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("lost cause")
	}
	if err.Error() != "gzip body: cause" {
		t.Fatal("changed diagnostic")
	}
}

func TestRejectionsEscapeTheEntryForDisplay(t *testing.T) {
	for _, test := range []struct {
		name, entry, want string
	}{
		{"plain name", "content/compose.yaml", "content/compose.yaml"},
		{"terminal escape", "content/a\x1b[31mb", `content/a\x1b[31mb`},
		{"newline", "content/a\nb", `content/a\nb`},
		{"invalid UTF-8", "content/\xff", `content/\xff`},
		{"quote", `content/"q"`, `content/\"q\"`},
		{"printable Unicode", "content/Ünïcode", "content/Ünïcode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := requireError(t, envelope.Reject(envelope.InvalidEnvelope, envelope.UnsafePath, test.entry, "rejected"))
			if failure.Entry != test.want {
				t.Fatalf("Entry = %q, want %q", failure.Entry, test.want)
			}
		})
	}
}

func TestRejectCauseEscapesEntryAndKeepsCause(t *testing.T) {
	cause := errors.New("short read")
	err := envelope.RejectCause(envelope.InvalidEnvelope, envelope.EntryContent, "content/\x07", cause, "entry content: %v", cause)
	failure := requireError(t, err)
	if !errors.Is(err, cause) || failure.Entry != `content/\a` || failure.Message != "entry content: short read" {
		t.Fatalf("unexpected rejection: %+v", failure)
	}
}

func requireError(t *testing.T, err error) *envelope.Error {
	t.Helper()
	failure, ok := errors.AsType[*envelope.Error](err)
	if !ok {
		t.Fatalf("expected *envelope.Error, got %v", err)
	}
	return failure
}
