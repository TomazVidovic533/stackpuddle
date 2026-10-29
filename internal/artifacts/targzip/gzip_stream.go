package targzip

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

func readGzip(source io.Reader, maxTarBytes, maxCompressedBytes int64) ([]byte, error) {
	compressed := &io.LimitedReader{R: source, N: maxCompressedBytes + 1}
	buffered := bufio.NewReader(compressed)
	rawHeader, err := buffered.Peek(10)
	if err != nil {
		return nil, gzipFailure(envelope.GzipHeader, "gzip header", err)
	}
	if rawHeader[3] != 0 || !bytes.Equal(rawHeader[4:8], []byte{0, 0, 0, 0}) || rawHeader[8] != 2 || rawHeader[9] != 255 {
		return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.GzipHeader, "", "noncanonical gzip header")
	}
	gzipReader, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, gzipFailure(envelope.GzipHeader, "gzip header", err)
	}
	gzipReader.Multistream(false)
	if !gzipReader.ModTime.IsZero() || gzipReader.OS != 255 || gzipReader.Name != "" || gzipReader.Comment != "" || len(gzipReader.Extra) != 0 {
		return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.GzipHeader, "", "noncanonical gzip header")
	}
	tarData, err := io.ReadAll(io.LimitReader(gzipReader, maxTarBytes+1))
	if compressed.N == 0 {
		return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.CompressedLimit, "", "compressed archive exceeds limit")
	}
	if err != nil {
		return nil, gzipFailure(envelope.GzipBody, "gzip body", err)
	}
	if int64(len(tarData)) > maxTarBytes {
		return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.ExpandedLimit, "", "expanded archive exceeds limit")
	}
	if err := gzipReader.Close(); err != nil {
		return nil, err
	}
	_, err = buffered.Peek(1)
	if compressed.N == 0 {
		return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.CompressedLimit, "", "compressed archive exceeds limit")
	}
	if err != io.EOF {
		if err == nil {
			return nil, envelope.Reject(envelope.InvalidEnvelope, envelope.TrailingCompressed, "", "trailing compressed bytes or multiple gzip members")
		}
		return nil, err
	}
	return tarData, nil
}

// Only known encoding failures are malformed input. Arbitrary source failures
// keep their original identity and are not misclassified as archive rejections.
func gzipFailure(code envelope.Code, operation string, cause error) error {
	wrapped := fmt.Errorf("%s: %w", operation, cause)
	var corrupt flate.CorruptInputError
	if errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF) || errors.Is(cause, gzip.ErrHeader) || errors.Is(cause, gzip.ErrChecksum) || errors.As(cause, &corrupt) {
		return &envelope.Error{Category: envelope.InvalidEnvelope, Code: code, Message: wrapped.Error(), Cause: cause}
	}
	return wrapped
}
