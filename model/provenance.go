package model

import (
	"slices"
)

// OriginKind says which authoring path produced a declaration.
type OriginKind int

const (
	OriginYAML OriginKind = iota + 1
	OriginSDK
)

func originKinds() []OriginKind { return []OriginKind{OriginYAML, OriginSDK} }

// String returns "yaml" or "sdk".
func (k OriginKind) String() string {
	switch k {
	case OriginYAML:
		return "yaml"
	case OriginSDK:
		return "sdk"
	default:
		return "unknown"
	}
}

// Origin is where a declaration or a patch was written.
type Origin struct {
	kind   OriginKind
	file   optional[string]
	line   int
	column int
}

// Kind returns the authoring path.
func (o Origin) Kind() OriginKind { return o.kind }

// File returns the source file relative to the project directory, if known.
func (o Origin) File() (string, bool) { return o.file.get() }

// Position returns the 1-based line and column of the declaration key, if known.
func (o Origin) Position() (line, column int, ok bool) { return o.line, o.column, o.line > 0 }

// Patch is a typed change an SDK applied to an imported declaration.
type Patch struct {
	origin Origin
	fields []string
}

// Origin returns where the patch was written.
func (p Patch) Origin() Origin { return p.origin }

// Fields returns the JSON pointers, relative to the object, of the members the patch set.
func (p Patch) Fields() []string { return slices.Clone(p.fields) }

// Provenance records where a declaration came from and every patch applied to it since.
type Provenance struct {
	declared Origin
	patches  []Patch
}

// Declared returns where the declaration was written.
func (p Provenance) Declared() Origin { return p.declared }

// Patches returns the patches in the order they were applied.
func (p Provenance) Patches() []Patch {
	patches := slices.Clone(p.patches)
	for index := range patches {
		patches[index].fields = slices.Clone(patches[index].fields)
	}
	return patches
}
