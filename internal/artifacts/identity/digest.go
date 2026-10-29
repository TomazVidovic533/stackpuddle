package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// Digest returns the SHA-256 identity of the exact bytes read.
func Digest(source io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return "", err
	}
	return formatDigest(hash.Sum(nil)), nil
}

func formatDigest(sum []byte) string {
	return "sha256:" + hex.EncodeToString(sum)
}
