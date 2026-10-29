package identity_test

import (
	"crypto/sha256"
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

func TestFormatIsTheDigestIdentityForm(t *testing.T) {
	const xSHA256 = "sha256:2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"
	sum := sha256.Sum256([]byte("x"))
	digest, err := identity.Digest(strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	if formatted := identity.Format(sum[:]); formatted != xSHA256 || digest != formatted {
		t.Fatalf("Format = %q, Digest = %q, want %q", formatted, digest, xSHA256)
	}
}
