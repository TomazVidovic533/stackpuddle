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
