package targzip

// The fixed gzip member header (RFC 1952, section 2.3) that canonical archives carry.
const (
	gzipHeaderSize       = 10
	gzipFlagsOffset      = 3
	gzipMTimeStart       = 4
	gzipMTimeEnd         = 8
	gzipExtraFlagsOffset = 8
	gzipExtraFlagsBest   = 2
	gzipOSOffset         = 9
	gzipOSUnknown        = 255
)

// canonicalGzipHeader reports whether a raw header sets no optional field, a zero modification
// time, the best-compression flag and an unknown operating system.
func canonicalGzipHeader(header []byte) bool {
	if header[gzipFlagsOffset] != 0 || header[gzipExtraFlagsOffset] != gzipExtraFlagsBest || header[gzipOSOffset] != gzipOSUnknown {
		return false
	}
	for _, value := range header[gzipMTimeStart:gzipMTimeEnd] {
		if value != 0 {
			return false
		}
	}
	return true
}
