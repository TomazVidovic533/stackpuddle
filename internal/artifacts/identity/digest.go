package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

const digestPrefix = "sha256:"

// Digest returns the SHA-256 identity of the exact bytes read.
func Digest(source io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return "", err
	}
	return Format(hash.Sum(nil)), nil
}

// Format renders a SHA-256 sum as the "sha256:"-prefixed lowercase hex identity. It is the one
// definition of that format; callers that hash while writing pass their sum here.
func Format(sum []byte) string {
	return digestPrefix + hex.EncodeToString(sum)
}
