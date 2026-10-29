// Package allocation_test measures Inspect's heap growth alone: no other test runs in this
// package, so nothing parallel skews the runtime's allocation counters.
package allocation_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/TomazVidovic533/stackpuddle/artifacts"
)

const (
	// zeroBombBytes expands beyond the default limits' tar ceiling while compressing to about 300 KB.
	zeroBombBytes = 300 << 20
	zeroChunkSize = 1 << 20
	// allocationCeiling is far below the expanded size, so buffering the stream cannot pass.
	allocationCeiling = 16 << 20
	gzipOSUnknown     = 255
)

func TestInspectRejectsZeroBombWithBoundedAllocation(t *testing.T) {
	bomb := zeroBomb(t)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := artifacts.Inspect(artifacts.Module, bytes.NewReader(bomb), artifacts.DefaultLimits())
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("compressed %d bytes expanding to %d bytes: Inspect allocated %d bytes (%.1f MiB) under DefaultLimits",
		len(bomb), zeroBombBytes, allocated, float64(allocated)/(1<<20))
	failure, ok := errors.AsType[*artifacts.Error](err)
	if !ok || failure.Category != artifacts.InvalidEnvelope || failure.Code != artifacts.ExpandedLimit {
		t.Fatalf("expected %s/%s, got %v", artifacts.InvalidEnvelope, artifacts.ExpandedLimit, err)
	}
	if allocated >= allocationCeiling {
		t.Fatalf("Inspect allocated %d bytes for a rejected archive, want below %d", allocated, allocationCeiling)
	}
}

// zeroBomb writes a canonical-header gzip of zeros from one reused buffer, so building it costs
// no allocation proportional to its expanded size.
func zeroBomb(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	writer.Header.ModTime = time.Unix(0, 0)
	writer.Header.OS = gzipOSUnknown
	zeros := make([]byte, zeroChunkSize)
	for range zeroBombBytes / zeroChunkSize {
		if _, err := writer.Write(zeros); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
