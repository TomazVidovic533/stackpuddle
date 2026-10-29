package envelope

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	streamSeparator  = ":"
	extensionMark    = "."
	superscriptOne   = '¹'
	superscriptTwo   = '²'
	superscriptThree = '³'
)

// ValidateExtractable rejects a path that ValidatePath accepts but that extraction could not
// reproduce on common filesystems: invalid UTF-8, control characters, NTFS stream syntax and
// Windows device names. Run ValidatePath first; this check assumes its segment rules hold.
func ValidateExtractable(name string) error {
	if !utf8.ValidString(name) {
		return Reject(InvalidEnvelope, UnsafePath, name, "unsafe archive path %q", name)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return Reject(InvalidEnvelope, ControlCharacter, name, "archive path %q contains a control character", name)
	}
	if strings.Contains(name, streamSeparator) {
		return Reject(InvalidEnvelope, StreamName, name, "archive path %q contains %q, which names an NTFS data stream", name, streamSeparator)
	}
	for segment := range strings.SplitSeq(name, "/") {
		if isDeviceName(segment) {
			return Reject(InvalidEnvelope, ReservedName, name, "archive path %q uses the Windows device name %q", name, segment)
		}
	}
	return nil
}

// Windows resolves a device name whatever extension follows it, so only the part before the
// first dot decides.
func isDeviceName(segment string) bool {
	stem, _, _ := strings.Cut(segment, extensionMark)
	for _, device := range []string{"CON", "PRN", "AUX", "NUL"} {
		if strings.EqualFold(stem, device) {
			return true
		}
	}
	port, size := utf8.DecodeLastRuneInString(stem)
	prefix := stem[:len(stem)-size]
	if !strings.EqualFold(prefix, "COM") && !strings.EqualFold(prefix, "LPT") {
		return false
	}
	return ('0' <= port && port <= '9') || port == superscriptOne || port == superscriptTwo || port == superscriptThree
}

// FoldedNames records archive names by Unicode simple case folding, so two entries that a
// case-insensitive filesystem would extract to one file are caught. Its memory grows with the
// names claimed. The zero value is not usable; call NewFoldedNames.
type FoldedNames struct {
	byKey map[string]string
}

// NewFoldedNames returns an empty set.
func NewFoldedNames() FoldedNames {
	return FoldedNames{byKey: make(map[string]string)}
}

// Claim records name. It rejects name with CaseCollision when an earlier, different name folds
// to the same key; an exact repeat is left to the duplicate-entry rules.
func (names FoldedNames) Claim(name string) error {
	key := foldKey(name)
	if earlier, exists := names.byKey[key]; exists && earlier != name {
		return Reject(InvalidEnvelope, CaseCollision, name, "archive path %q differs from %q only by case", name, earlier)
	}
	names.byKey[key] = name
	return nil
}

func foldKey(name string) string {
	var key strings.Builder
	key.Grow(len(name))
	for _, r := range name {
		key.WriteRune(smallestFold(r))
	}
	return key.String()
}

// smallestFold maps every rune of one SimpleFold orbit, such as k, K and the Kelvin sign, to the
// same representative.
func smallestFold(r rune) rune {
	smallest := r
	for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
		smallest = min(smallest, folded)
	}
	return smallest
}
