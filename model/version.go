package model

import (
	"strings"
)

// APIVersion is the one contract version this library reads and writes.
const APIVersion = "stackpuddle/v1"

// SupportedAPIVersions returns the API versions this library reads, oldest first. Safe for
// concurrent use; each call returns a new slice.
func SupportedAPIVersions() []string {
	return []string{APIVersion}
}

// Version is a Semantic Versioning 2.0.0 version. Build metadata is kept for String but ignored
// by Compare, as the specification requires. The zero value is not a valid version.
type Version struct {
	core       [3]string
	prerelease []string
	build      string
}

const (
	versionCoreParts = 3
	prereleaseMark   = '-'
	buildMark        = '+'
)

// ParseVersion parses s as a SemVer 2.0.0 version with no "v" prefix. Numeric identifiers may be
// arbitrarily large. It returns an *Error with code structure.invalid_value when s is not one.
func ParseVersion(s string) (Version, error) {
	rest, build, hasBuild := strings.Cut(s, string(buildMark))
	if hasBuild && !validBuild(build) {
		return Version{}, invalidValue("version %q has invalid build metadata", s)
	}
	core, prerelease, hasPrerelease := strings.Cut(rest, string(prereleaseMark))
	if hasPrerelease && !validPrerelease(prerelease) {
		return Version{}, invalidValue("version %q has an invalid prerelease", s)
	}
	parts := strings.Split(core, ".")
	if len(parts) != versionCoreParts {
		return Version{}, invalidValue("version %q needs major.minor.patch", s)
	}
	var version Version
	for index, part := range parts {
		if !isNumericIdentifier(part) {
			return Version{}, invalidValue("version %q has an invalid number in major.minor.patch", s)
		}
		version.core[index] = part
	}
	if hasPrerelease {
		version.prerelease = strings.Split(prerelease, ".")
	}
	version.build = build
	return version, nil
}

// validPrerelease checks dot-separated identifiers where a numeric one carries no leading zero.
func validPrerelease(text string) bool {
	for _, identifier := range strings.Split(text, ".") {
		if !isIdentifier(identifier) || (isDigits(identifier) && !isNumericIdentifier(identifier)) {
			return false
		}
	}
	return true
}

// validBuild checks dot-separated identifiers; unlike a prerelease, build metadata may have leading
// zeros.
func validBuild(text string) bool {
	for _, identifier := range strings.Split(text, ".") {
		if !isIdentifier(identifier) {
			return false
		}
	}
	return true
}

// isIdentifier reports whether text is one non-empty SemVer identifier of [0-9A-Za-z-].
func isIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if !isIdentifierChar(ch) {
			return false
		}
	}
	return true
}

func isIdentifierChar(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '-'
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func isNumericIdentifier(text string) bool {
	return isDigits(text) && (len(text) == 1 || text[0] != '0')
}

// Compare returns -1, 0 or +1 as v sorts before, equal to or after o under SemVer 2.0.0 section
// 11: numeric identifiers compare as numbers, so 1.0.0-alpha.9 sorts before 1.0.0-alpha.10.
func (v Version) Compare(o Version) int {
	for index := range v.core {
		if c := compareNumeric(v.core[index], o.core[index]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.prerelease) == 0 && len(o.prerelease) == 0:
		return 0
	case len(v.prerelease) == 0:
		return 1
	case len(o.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(v.prerelease) && index < len(o.prerelease); index++ {
		if c := comparePrerelease(v.prerelease[index], o.prerelease[index]); c != 0 {
			return c
		}
	}
	return compareInts(len(v.prerelease), len(o.prerelease))
}

func comparePrerelease(a, b string) int {
	aNumeric, bNumeric := isDigits(a), isDigits(b)
	switch {
	case aNumeric && bNumeric:
		return compareNumeric(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// compareNumeric compares decimal digit strings without leading zeros by value, at any length.
func compareNumeric(a, b string) int {
	if c := compareInts(len(a), len(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// String returns the version as parsed, build metadata included.
func (v Version) String() string {
	var builder strings.Builder
	builder.WriteString(strings.Join(v.core[:], "."))
	if len(v.prerelease) > 0 {
		builder.WriteRune(prereleaseMark)
		builder.WriteString(strings.Join(v.prerelease, "."))
	}
	if v.build != "" {
		builder.WriteRune(buildMark)
		builder.WriteString(v.build)
	}
	return builder.String()
}
