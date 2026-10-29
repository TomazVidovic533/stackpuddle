package targzip

import (
	"io"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

// tarTracker sits between the expanded gzip stream and tar.Reader, which reads exactly what it
// parses, so the tracker sees the same bytes in the same order. It keeps fixed state only: the
// offset, the last two blocks (the raw header tar.Reader just consumed, and the end markers),
// one padding range that must be zero, and the first read error, kept apart from tar's errors.
type tarTracker struct {
	source  io.Reader
	offset  int64
	tail    [tarEndMarkerSize]byte
	padding paddingRange
	readErr error
}

// paddingRange is the zero fill after one entry's content, checked as it streams past.
type paddingRange struct {
	start, end int64
	entry      string
	nonzero    bool
}

func newTarTracker(source io.Reader) *tarTracker {
	return &tarTracker{source: source}
}

// Read passes reads through. An io.Reader returns io.EOF itself at the end, so a wrapped EOF is a
// failure here, as io.ReadAll and io.ReadFull treat it.
func (tracker *tarTracker) Read(p []byte) (int, error) {
	n, err := tracker.source.Read(p)
	tracker.observe(p[:n])
	if err != nil && err != io.EOF && tracker.readErr == nil {
		tracker.readErr = err
	}
	return n, err
}

// drain reads to the end of the expanded stream, so gzip-level failures past a tar failure are
// still seen. Read records the only error io.Copy can return here, and finish classifies it.
func (tracker *tarTracker) drain() {
	_, _ = io.Copy(io.Discard, tracker)
}

func (tracker *tarTracker) observe(data []byte) {
	start := tracker.offset
	tracker.offset += int64(len(data))
	tracker.checkPadding(start, data)
	if len(data) >= len(tracker.tail) {
		copy(tracker.tail[:], data[len(data)-len(tracker.tail):])
		return
	}
	copy(tracker.tail[:], tracker.tail[len(data):])
	copy(tracker.tail[len(tracker.tail)-len(data):], data)
}

func (tracker *tarTracker) checkPadding(start int64, data []byte) {
	padding := &tracker.padding
	low, high := max(start, padding.start), min(start+int64(len(data)), padding.end)
	if padding.nonzero || low >= high {
		return
	}
	padding.nonzero = !allZero(data[low-start : high-start])
}

// expectPadding registers the padding of an entry whose content starts at the current offset.
func (tracker *tarTracker) expectPadding(entry string, size int64) {
	start := tracker.offset + size
	tracker.padding = paddingRange{start: start, end: start + (tarBlockSize-size%tarBlockSize)%tarBlockSize, entry: entry}
}

// entryEnd is the offset after the registered entry's content and padding.
func (tracker *tarTracker) entryEnd() int64 {
	return tracker.padding.end
}

func (tracker *tarTracker) paddingFailure() error {
	if !tracker.padding.nonzero {
		return nil
	}
	name := tracker.padding.entry
	return envelope.Reject(envelope.InvalidEnvelope, envelope.Padding, name, "nonzero or truncated padding for %q", name)
}

// lastBlock is the 512 bytes read last: right after tar.Reader.Next, the raw header it parsed.
func (tracker *tarTracker) lastBlock() []byte {
	return tracker.tail[tarBlockSize:]
}

// hasEndMarkers reports whether the stream ends in two zero blocks on a block boundary.
func (tracker *tarTracker) hasEndMarkers() bool {
	return tracker.offset >= tarEndMarkerSize && tracker.offset%tarBlockSize == 0 && allZero(tracker.tail[:])
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}
