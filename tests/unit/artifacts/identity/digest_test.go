package identity_test

import (
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
)

func TestDigestUsesCompressedBytes(t *testing.T) {
	const emptySHA256 = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	empty, err := identity.Digest(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if empty != emptySHA256 {
		t.Fatalf("empty digest = %q", empty)
	}
	changed, err := identity.Digest(strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	if changed == empty {
		t.Fatal("changed bytes retained the same digest")
	}
}
