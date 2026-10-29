package envelope_test

import (
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

func TestValidatePathAcceptsCanonicalModulePaths(t *testing.T) {
	for _, path := range []string{"module.yaml", "content/compose.yaml", "content/bin/tool"} {
		if err := envelope.ValidatePath(path); err != nil {
			t.Errorf("%q: %v", path, err)
		}
	}
}

func TestValidatePathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", "/absolute", "../escape", "content/../escape", "content//file", "content/./file", "content\\file", "content/\x00file"} {
		err := envelope.ValidatePath(path)
		artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.UnsafePath)
	}
}

func TestLayoutAllowsOnlyItsDescriptorAndModuleContent(t *testing.T) {
	for _, test := range []struct {
		kind       envelope.Kind
		descriptor string
		name       string
		allowed    bool
	}{
		{envelope.Module, "module.yaml", "module.yaml", true},
		{envelope.Module, "module.yaml", "content", true},
		{envelope.Module, "module.yaml", "content/file", true},
		{envelope.Module, "module.yaml", "content-other", false},
		{envelope.Module, "module.yaml", "bundle.yaml", false},
		{envelope.Bundle, "bundle.yaml", "bundle.yaml", true},
		{envelope.Bundle, "bundle.yaml", "content/file", false},
	} {
		layout, err := envelope.NewLayout(test.kind)
		if err != nil {
			t.Fatal(err)
		}
		if layout.Descriptor() != test.descriptor || layout.Allows(test.name) != test.allowed {
			t.Fatalf("unexpected layout for %s: %s", test.kind, test.name)
		}
	}
	var zero envelope.Layout
	if zero.Allows("") || zero.Allows("module.yaml") {
		t.Fatal("zero layout allowed a path")
	}
}
