package artifacts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

type Entry struct {
	Name string
	Data string
	Kind byte
	Mode int64
	UID  int
}

func Archive(t testing.TB, entries ...Entry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	gzipWriter.Header.OS = 255
	tw := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.Kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.Mode
		if mode == 0 {
			if kind == tar.TypeDir {
				mode = 0755
			} else {
				mode = 0644
			}
		}
		header := &tar.Header{Name: entry.Name, Mode: mode, Uid: entry.UID, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR, Typeflag: kind, Size: int64(len(entry.Data))}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, entry.Data[:header.Size]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func Gzip(t testing.TB, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	gzipWriter.Header.OS = 255
	if _, err := gzipWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func ModuleSource(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "module.yaml"), []byte("name: postgres\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "content", "compose.yaml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

// Decompress exposes tar bytes so each test keeps its mutation visible.
func Decompress(t testing.TB, archive []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return data
}

// RepairChecksum lets a raw-header mutation reach its intended semantic check.
func RepairChecksum(t testing.TB, header []byte) {
	t.Helper()
	if len(header) != 512 {
		t.Fatal("checksum requires one tar header")
	}
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	sum := 0
	for _, value := range header {
		sum += int(value)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
}
func RequireFailure(t testing.TB, err error, category envelope.Category, code envelope.Code) {
	t.Helper()
	var failure *envelope.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected %s/%s, got %v", category, code, err)
	}
	if failure.Category != category || failure.Code != code {
		t.Fatalf("expected %s/%s, got %+v", category, code, failure)
	}
}
