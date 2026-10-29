package targzip

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/identity"
)

// Build writes a canonical archive and returns the digest of its compressed bytes: it collects
// the source, then writes it. A file that changes between the two steps is refused.
func Build(kind envelope.Kind, sourceDir string, dst io.Writer) (string, error) {
	source, err := Collect(kind, sourceDir)
	if err != nil {
		return "", err
	}
	digest, err := source.Write(dst)
	if closeErr := source.Close(); closeErr != nil {
		return "", errors.Join(err, closeErr)
	}
	if err != nil {
		return "", err
	}
	return digest, nil
}

// Write writes the collected entries as a canonical archive and returns the digest of its
// compressed bytes. Each file is read through the source's root handle and must still exist as
// the file, with the size, that Collect walked; otherwise Write fails with SourceChanged.
func (source Source) Write(dst io.Writer) (string, error) {
	hash := sha256.New()
	gzipWriter, err := gzip.NewWriterLevel(io.MultiWriter(dst, hash), gzip.BestCompression)
	if err != nil {
		return "", err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0)
	gzipWriter.Header.OS = gzipOSUnknown
	tarWriter := tar.NewWriter(gzipWriter)
	descriptor := source.layout.Descriptor()
	for _, entry := range source.entries {
		if err := source.writeEntry(tarWriter, entry, descriptor); err != nil {
			return "", err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}
	return identity.Format(hash.Sum(nil)), nil
}

func (source Source) writeEntry(writer *tar.Writer, entry sourceEntry, descriptor string) error {
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
		return writeHeader(writer, header, entry.name)
	}
	if entry.name != descriptor && entry.info.Mode()&0111 != 0 {
		header.Mode = 0755
	}
	file, err := source.open(entry)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := writeHeader(writer, header, entry.name); err != nil {
		return err
	}
	return copyContent(writer, file, entry)
}

// open opens a walked file through the root and refuses it unless it is still the regular file
// Collect saw: Lstat refuses a symlink swapped in before the root resolves it, and Stat on the
// handle refuses a file replaced between the two calls. copyContent then holds the walked size.
func (source Source) open(entry sourceEntry) (*os.File, error) {
	name := filepath.FromSlash(entry.name)
	current, err := source.root.Lstat(name)
	if err != nil {
		return nil, changedOrRaw(entry.name, err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(entry.info, current) {
		return nil, sourceChanged(entry.name)
	}
	file, err := source.root.Open(name)
	if err != nil {
		return nil, changedOrRaw(entry.name, err)
	}
	opened, err := file.Stat()
	if err == nil && !os.SameFile(entry.info, opened) {
		err = sourceChanged(entry.name)
	}
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return file, nil
}

// copyContent copies exactly the walked size and refuses a file that shrank or grew meanwhile.
func copyContent(writer io.Writer, file *os.File, entry sourceEntry) error {
	if _, err := io.CopyN(writer, file, entry.info.Size()); err != nil {
		if errors.Is(err, io.EOF) {
			return sourceChanged(entry.name)
		}
		return fmt.Errorf("write %q: %w", entry.name, err)
	}
	var extra [1]byte
	n, err := file.Read(extra[:])
	if n > 0 {
		return sourceChanged(entry.name)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read %q: %w", entry.name, err)
	}
	return nil
}

func writeHeader(writer *tar.Writer, header *tar.Header, name string) error {
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write %q header: %w", name, err)
	}
	return nil
}

const sourceChangedFormat = "source entry %q changed after it was collected"

func sourceChanged(name string) error {
	return envelope.Reject(envelope.InvalidArgument, envelope.SourceChanged, name, sourceChangedFormat, name)
}

// changedOrRaw classifies a failed Lstat or Open of a walked file by the error alone. Collect and
// the Lstat in open proved the shape, so a failure that is not an operating-system errno (os.Root's
// unexported escape refusal) or is ELOOP, ENOTDIR or ENOENT can only come from a source change:
// SourceChanged, with err kept as the cause. Any other errno (EACCES, EMFILE, EIO) stays raw.
func changedOrRaw(name string, err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != syscall.ELOOP && errno != syscall.ENOTDIR && errno != syscall.ENOENT {
		return err
	}
	return envelope.RejectCause(envelope.InvalidArgument, envelope.SourceChanged, name, err, sourceChangedFormat, name)
}
