package envelope

import (
	"strings"
)

// Kind identifies the descriptor and permitted payload layout of an archive.
type Kind string

const (
	Module Kind = "module"
	Bundle Kind = "bundle"
)

// NewLayout resolves the descriptor and path rules for a supported kind.
func NewLayout(kind Kind) (Layout, error) {
	switch kind {
	case Module:
		return Layout{kind: kind, descriptor: "module.yaml"}, nil
	case Bundle:
		return Layout{kind: kind, descriptor: "bundle.yaml"}, nil
	default:
		return Layout{}, Reject(InvalidArgument, UnknownKind, "", "unknown archive kind %q", kind)
	}
}

// ValidatePath rejects paths that are ambiguous or unsafe in an envelope.
func ValidatePath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return Reject(InvalidEnvelope, UnsafePath, name, "unsafe archive path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return Reject(InvalidEnvelope, UnsafePath, name, "unsafe archive path %q", name)
		}
	}
	return nil
}

// Layout binds a supported kind to its descriptor and permitted paths.
// Obtain a layout through NewLayout before using it.
type Layout struct {
	kind       Kind
	descriptor string
}

func (layout Layout) Descriptor() string { return layout.descriptor }
func (layout Layout) Allows(name string) bool {
	if layout.descriptor == "" {
		return false
	}
	return name == layout.descriptor || (layout.kind == Module && (name == "content" || strings.HasPrefix(name, "content/")))
}
