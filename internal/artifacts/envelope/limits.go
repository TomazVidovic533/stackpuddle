package envelope

// Limits bound archive inspection before content is trusted or extracted.
type Limits struct {
	DescriptorBytes int64
	FileBytes       int64
	TotalBytes      int64
	Entries         int
	PathBytes       int
	PathDepth       int
}

// DefaultLimits returns the limits Build enforces and callers start from. Each call returns a
// new value, so no caller can change what another reads.
func DefaultLimits() Limits {
	return Limits{
		DescriptorBytes: 1 << 20,
		FileBytes:       64 << 20,
		TotalBytes:      256 << 20,
		Entries:         4096,
		PathBytes:       240,
		PathDepth:       32,
	}
}

// Bounds validates limits and returns tar and compressed byte ceilings.
func (limits Limits) Bounds() (int64, int64, error) {
	if limits.DescriptorBytes <= 0 || limits.FileBytes <= 0 || limits.TotalBytes <= 0 || limits.Entries <= 0 || limits.PathBytes <= 0 || limits.PathDepth <= 0 {
		return 0, 0, Reject(InvalidArgument, NonpositiveLimits, "", "archive limits must be positive")
	}

	const maxInt64 = int64(^uint64(0) >> 1)
	if int64(limits.Entries) > maxInt64/1024-1 {
		return 0, 0, Reject(InvalidArgument, LimitsOverflow, "", "archive limits overflow")
	}
	// Each entry needs a 512-byte header and up to 511 padding bytes.
	// Reserve two full blocks per entry plus the two end-marker blocks.
	headerAllowance := (int64(limits.Entries) + 1) * 1024
	if limits.TotalBytes > maxInt64-headerAllowance {
		return 0, 0, Reject(InvalidArgument, LimitsOverflow, "", "archive limits overflow")
	}
	maxTarBytes := limits.TotalBytes + headerAllowance
	// Allow 6.25% plus 64 KiB for DEFLATE expansion and framing.
	// The extra byte below lets inspection distinguish exact limits from overflow.
	compressionAllowance := maxTarBytes/16 + 64<<10
	if maxTarBytes > maxInt64-compressionAllowance-1 {
		return 0, 0, Reject(InvalidArgument, LimitsOverflow, "", "archive limits overflow")
	}
	return maxTarBytes, maxTarBytes + compressionAllowance, nil
}
