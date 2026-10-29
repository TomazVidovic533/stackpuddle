package targzip

import (
	"archive/tar"
	"bytes"
	"strings"
	"time"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
)

const (
	tarBlockSize       = 512
	tarEndMarkerSize   = 2 * tarBlockSize
	ustarNameEnd       = 100
	ustarTypeOffset    = 156
	ustarPrefixStart   = 345
	ustarReservedStart = 500
)

func validateRawHeader(block []byte, name string) error {
	if hasHiddenTarNameBytes(block) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.TarName, "", "noncanonical tar name")
	}
	if rawType := block[ustarTypeOffset]; rawType != tar.TypeReg && rawType != tar.TypeDir {
		if rawType == tar.TypeRegA {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.TarType, name, "noncanonical tar type for %q", name)
		}
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryType, name, "unsupported tar entry type for %q", name)
	}
	if !bytes.Equal(block[ustarReservedStart:tarBlockSize], make([]byte, tarBlockSize-ustarReservedStart)) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.ReservedBytes, name, "noncanonical USTAR reserved bytes for %q", name)
	}
	return nil
}
func validateParentDirectories(name string, seen map[string]bool) error {
	for parent := name; strings.Contains(parent, "/"); {
		parent = parent[:strings.LastIndex(parent, "/")]
		if isDir, exists := seen[parent]; exists && !isDir {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryCollision, parent, "file/directory collision at %q", parent)
		}
	}
	return nil
}
func validatePadding(data []byte, headerOffset int, size int64, name string) error {
	paddingStart := headerOffset + tarBlockSize + int(size)
	paddingBytes := int((tarBlockSize - size%tarBlockSize) % tarBlockSize)
	if paddingStart < 0 || paddingStart+paddingBytes > len(data) || !bytes.Equal(data[paddingStart:paddingStart+paddingBytes], make([]byte, paddingBytes)) {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.Padding, name, "nonzero or truncated padding for %q", name)
	}
	return nil
}
func hasHiddenTarNameBytes(header []byte) bool {
	for _, field := range [][]byte{header[:ustarNameEnd], header[ustarPrefixStart:ustarReservedStart]} {
		if terminator := bytes.IndexByte(field, 0); terminator >= 0 && !bytes.Equal(field[terminator+1:], make([]byte, len(field)-terminator-1)) {
			return true
		}
	}
	return false
}

func inspectHeader(header *tar.Header, descriptor bool) error {
	if header.Format != tar.FormatUSTAR || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" ||
		!header.ModTime.Equal(time.Unix(0, 0)) || !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() ||
		header.Linkname != "" || len(header.PAXRecords) != 0 || header.Devmajor != 0 || header.Devminor != 0 {
		return envelope.Reject(envelope.InvalidEnvelope, envelope.Metadata, header.Name, "noncanonical tar metadata for %q", header.Name)
	}
	switch header.Typeflag {
	case tar.TypeReg:
		if strings.HasSuffix(header.Name, "/") || (header.Mode != 0644 && (descriptor || header.Mode != 0755)) {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.RegularEntry, header.Name, "noncanonical regular entry %q", header.Name)
		}
	case tar.TypeDir:
		if descriptor || !strings.HasSuffix(header.Name, "/") || header.Mode != 0755 || header.Size != 0 {
			return envelope.Reject(envelope.InvalidEnvelope, envelope.DirectoryEntry, header.Name, "noncanonical directory %q", header.Name)
		}
	default:
		return envelope.Reject(envelope.InvalidEnvelope, envelope.EntryType, header.Name, "unsupported tar entry type for %q", header.Name)
	}
	return nil
}
