package targzip

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// Build writes a canonical archive and returns the digest of its compressed bytes.
// The local source tree must remain unchanged while Build reads it.
func Build(kind envelope.Kind, sourceDir string, dst io.Writer) (string, error) {
	layout, err := envelope.NewLayout(kind)
	if err != nil {
		return "", err
	}
	entries, err := collectSource(kind, sourceDir, layout)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	gzipWriter, err := gzip.NewWriterLevel(io.MultiWriter(dst, hash), gzip.BestCompression)
	if err != nil {
		return "", err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := writeEntry(tarWriter, entry); err != nil {
			return "", err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeEntry(writer *tar.Writer, entry sourceEntry) error {
	header := &tar.Header{
		Name:     entry.name,
		Mode:     0644,
		Size:     entry.info.Size(),
		ModTime:  time.Unix(0, 0),
		Typeflag: tar.TypeReg,
		Format:   tar.FormatUSTAR,
	}
	if entry.info.IsDir() {
		header.Name += "/"
		header.Mode = 0755
		header.Size = 0
		header.Typeflag = tar.TypeDir
	} else if entry.name != "module.yaml" && entry.name != "bundle.yaml" && entry.info.Mode()&0111 != 0 {
		header.Mode = 0755
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write %q header: %w", entry.name, err)
	}
	if entry.info.IsDir() {
		return nil
	}
	file, err := os.Open(entry.path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("write %q: %w", entry.name, err)
	}
	return nil
}
