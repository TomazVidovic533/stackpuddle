package model

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Binding connects a consumer's Dependency to an Export. Its identity is Ref: the consumer and the
// Dependency key.
type Binding struct {
	ref        BindingRef
	source     ExportRef
	parameters optional[[]byte]
	delivery   optional[Delivery]
	at         origin
}

// Ref returns the binding's identity.
func (b Binding) Ref() BindingRef { return b.ref }

// Source returns the Export the binding consumes.
func (b Binding) Source() ExportRef { return b.source }

// Parameters returns the canonical JSON bytes of the module-defined binding parameters, if any.
func (b Binding) Parameters() (json.RawMessage, bool) {
	parameters, ok := b.parameters.get()
	return slices.Clone(parameters), ok
}

// Delivery returns how the Export's values reach the consumer, if the binding declares it.
func (b Binding) Delivery() (Delivery, bool) { return b.delivery.get() }

// Delivery maps an Export's values onto a consumer's environment variables and files.
type Delivery struct {
	environment []EnvDelivery
	files       []FileDelivery
}

// Environment returns the environment variables the delivery sets, ordered by variable name.
func (d Delivery) Environment() []EnvDelivery { return slices.Clone(d.environment) }

// Files returns the files the delivery writes, ordered by value name.
func (d Delivery) Files() []FileDelivery { return slices.Clone(d.files) }

// EnvDelivery sets one environment variable to one Export value.
type EnvDelivery struct {
	Key   EnvKey
	Value ValueName
}

// FileDelivery writes one Export value to a file in the consumer's container.
type FileDelivery struct {
	Value ValueName
	Path  string
	Mode  FileMode
	owner optional[string]
}

// Owner returns the file's "<uid>:<gid>" owner, if one is declared.
func (f FileDelivery) Owner() (string, bool) { return f.owner.get() }

// FileMode is the permission mode of a delivered file. The schema admits read-only and
// owner-writable modes only.
type FileMode uint32

// String returns the mode as four octal digits, such as "0440".
func (m FileMode) String() string { return fmt.Sprintf("%04o", uint32(m)) }
