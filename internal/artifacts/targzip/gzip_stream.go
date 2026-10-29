package targzip

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// gzipStream is one canonical gzip member read within the compressed and expanded ceilings.
// Inspect reads its expanded bytes as a stream and calls finish once they are drained.
type gzipStream struct {
	compressed  *io.LimitedReader
	buffered    *bufio.Reader
	reader      *gzip.Reader
	maxTarBytes int64
}

func openGzip(source io.Reader, maxTarBytes, maxCompressedBytes int64) (gzipStream, error) {
	compressed := &io.LimitedReader{R: source, N: maxCompressedBytes + 1}
	buffered := bufio.NewReader(compressed)
	rawHeader, err := buffered.Peek(gzipHeaderSize)
	if err != nil {
		return gzipStream{}, gzipFailure(envelope.GzipHeader, "gzip header", err)
	}
	if !canonicalGzipHeader(rawHeader) {
		return gzipStream{}, envelope.Reject(envelope.InvalidEnvelope, envelope.GzipHeader, "", "noncanonical gzip header")
	}
	reader, err := gzip.NewReader(buffered)
	if err != nil {
		return gzipStream{}, gzipFailure(envelope.GzipHeader, "gzip header", err)
	}
	reader.Multistream(false)
	if !reader.ModTime.IsZero() || reader.OS != gzipOSUnknown || reader.Name != "" || reader.Comment != "" || len(reader.Extra) != 0 {
		return gzipStream{}, envelope.Reject(envelope.InvalidEnvelope, envelope.GzipHeader, "", "noncanonical gzip header")
	}
	return gzipStream{compressed: compressed, buffered: buffered, reader: reader, maxTarBytes: maxTarBytes}, nil
}

// expanded yields the decompressed bytes, capped one byte past the tar ceiling so finish can
// tell an exact fit from an overflow without reading further.
func (stream gzipStream) expanded() io.Reader {
	return io.LimitReader(stream.reader, stream.maxTarBytes+1)
}

// finish classifies the member after its expanded bytes were drained: readErr is the first
// error other than io.EOF that expanded returned, and expandedBytes how many bytes it yielded.
// The checks keep the order a fully buffered read had, so precedence between failures holds.
func (stream gzipStream) finish(readErr error, expandedBytes int64) error {
	if stream.compressed.N == 0 {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.CompressedLimit, "", "compressed archive exceeds limit")
	}
	if readErr != nil {
		return gzipFailure(envelope.GzipBody, "gzip body", readErr)
	}
	if expandedBytes > stream.maxTarBytes {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.ExpandedLimit, "", "expanded archive exceeds limit")
	}
	if err := stream.reader.Close(); err != nil {
		return err
	}
	_, err := stream.buffered.Peek(1)
	if stream.compressed.N == 0 {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.CompressedLimit, "", "compressed archive exceeds limit")
	}
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.TrailingCompressed, "", "trailing compressed bytes or multiple gzip members")
	}
	return err
}

// Only known encoding failures are malformed input. Arbitrary source failures
// keep their original identity and are not misclassified as archive rejections.
func gzipFailure(code envelope.Code, operation string, cause error) error {
	_, corrupt := errors.AsType[flate.CorruptInputError](cause)
	if corrupt || errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrUnexpectedEOF) || errors.Is(cause, gzip.ErrHeader) || errors.Is(cause, gzip.ErrChecksum) {
		return envelope.RejectCause(envelope.InvalidEnvelope, code, "", cause, "%s: %v", operation, cause)
	}
	return fmt.Errorf("%s: %w", operation, cause)
}
