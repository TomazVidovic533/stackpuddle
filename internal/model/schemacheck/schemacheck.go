// Package schemacheck compiles JSON Schemas without any file or network access and reports
// validation failures as one leaf violation each.
package schemacheck

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math/big"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/TomazVidovic533/stackpuddle/internal/model/yamlvalue"
)

// Kind classifies a violation.
type Kind int

const (
	UnknownField Kind = iota + 1
	MissingField
	InvalidType
	InvalidValue
	ExactlyOne
)

// Violation is one leaf failure. Pointer names the failing member; for MissingField it is where the
// member belongs. Field is the member name for UnknownField and MissingField. Expected describes the
// constraint from the schema alone and never contains the checked value.
type Violation struct {
	Pointer  string
	Field    string
	Kind     Kind
	Expected string
}

// Result is what one Check found: at most the limit's leaf violations, in pointer order, and
// whether Check stopped at that limit with more failures left to report.
type Result struct {
	Violations []Violation
	Truncated  bool
}

// ErrLoadRefused is the cause of every compile failure that needed a schema the set does not hold.
var ErrLoadRefused = errors.New("schema loading is disabled")

// refusingLoader keeps every reference inside the resources given to the compiler: the library's
// default loader reads file URLs, so a module's option schema could otherwise read local files. It
// records the refusal because the library's load error does not unwrap to its cause.
type refusingLoader struct {
	refused bool
}

func (l *refusingLoader) Load(url string) (any, error) {
	l.refused = true
	return nil, fmt.Errorf("%w: %s", ErrLoadRefused, url)
}

// Set holds compiled schemas by identifier. It is safe for concurrent use.
type Set struct {
	schemas map[string]*jsonschema.Schema
}

// CompileFS compiles every "*.schema.json" file under dir in fsys. Each file must carry a string
// "$id"; the set is keyed by it.
func CompileFS(fsys fs.FS, dir string) (*Set, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.schema.json"))
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	documents := make(map[string]any, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", name, err)
		}
		document, err := jsonschema.UnmarshalJSON(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("decode schema %s: %w", name, err)
		}
		object, ok := document.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema %s is not an object", name)
		}
		id, ok := object["$id"].(string)
		if !ok {
			return nil, fmt.Errorf("schema %s has no $id", name)
		}
		documents[id] = document
	}
	return compile(documents)
}

// CompileDocument compiles one schema tree, as decoded by yamlvalue, under the identifier id.
func CompileDocument(id string, document yamlvalue.Value) (*Set, error) {
	return compile(map[string]any{id: document})
}

func compile(documents map[string]any) (*Set, error) {
	compiler := jsonschema.NewCompiler()
	loader := &refusingLoader{}
	compiler.UseLoader(loader)
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	ids := slices.Sorted(maps.Keys(documents))
	for _, id := range ids {
		if err := compiler.AddResource(id, documents[id]); err != nil {
			return nil, fmt.Errorf("add schema %s: %w", id, err)
		}
	}
	set := &Set{schemas: make(map[string]*jsonschema.Schema, len(ids))}
	for _, id := range ids {
		schema, err := compiler.Compile(id)
		if err != nil && loader.refused {
			return nil, fmt.Errorf("compile schema %s: %w (%v)", id, ErrLoadRefused, err)
		}
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", id, err)
		}
		set.schemas[id] = schema
	}
	return set, nil
}

// Has reports whether the set holds a schema with the identifier id.
func (s *Set) Has(id string) bool {
	_, ok := s.schemas[id]
	return ok
}

// Check validates value, a tree decoded by yamlvalue, against the schema id, keeping at most limit
// violations; limit must be positive. It returns an error when the set has no such schema.
func (s *Set) Check(id string, value yamlvalue.Value, limit int) (Result, error) {
	schema, ok := s.schemas[id]
	if !ok {
		return Result{}, fmt.Errorf("no schema %s", id)
	}
	err := schema.Validate(value)
	if err == nil {
		return Result{}, nil
	}
	var validationError *jsonschema.ValidationError
	if !errors.As(err, &validationError) {
		return Result{}, fmt.Errorf("validate against %s: %w", id, err)
	}
	walker := flattener{instance: value, limit: limit}
	walker.flatten(validationError, nil)
	return Result{Violations: dedupe(walker.violations), Truncated: walker.truncated}, nil
}

// flattener turns the validator's error tree into leaf violations. It stops at limit, so the
// pointers it joins, each up to the nesting depth times the key length long, stay bounded too. It
// walks failures in a fixed order, so a report cut at the limit keeps the same violations on every
// run although the validator visits object members in map order.
type flattener struct {
	instance   any
	limit      int
	violations []Violation
	truncated  bool
}

func (f *flattener) add(violation Violation) {
	if len(f.violations) >= f.limit {
		f.truncated = true
		return
	}
	f.violations = append(f.violations, violation)
}

// flatten walks one error node. trusted is the instance location of the nearest ancestor whose own
// location is reliable; see addPropertyNames for the one kind whose location is not.
func (f *flattener) flatten(err *jsonschema.ValidationError, trusted []string) {
	if f.truncated {
		return
	}
	switch err.ErrorKind.(type) {
	case *kind.Schema, *kind.Group, *kind.Reference, *kind.AllOf:
		causes := primaryCauses(err.Causes)
		slices.SortStableFunc(causes, compareCauses)
		for _, cause := range causes {
			f.flatten(cause, err.InstanceLocation)
		}
	default:
		f.leaf(err, trusted)
	}
}

// leaf adds the violations of one failure. Its pointer is joined here, once per leaf, never for
// the composite failures above it.
func (f *flattener) leaf(err *jsonschema.ValidationError, trusted []string) {
	at := pointerOf(err.InstanceLocation)
	switch k := err.ErrorKind.(type) {
	case *kind.OneOf:
		// The alternatives' own failures describe branches the author did not choose; one
		// violation for the choice keeps the report to the root cause.
		f.add(Violation{Pointer: at, Kind: ExactlyOne, Expected: "exactly one of the alternatives the schema lists"})
	case *kind.AnyOf:
		f.add(Violation{Pointer: at, Kind: InvalidValue, Expected: "one of the alternatives the schema lists"})
	case *kind.Required:
		f.addFields(at, k.Missing, MissingField)
	case *kind.AdditionalProperties:
		f.addFields(at, slices.Sorted(slices.Values(k.Properties)), UnknownField)
	case *kind.FalseSchema:
		if isUnevaluated(err) {
			f.add(Violation{Pointer: at, Field: lastToken(err.InstanceLocation), Kind: UnknownField})
			return
		}
		f.add(Violation{Pointer: at, Kind: InvalidValue, Expected: "no value here"})
	case *kind.Type:
		f.add(Violation{Pointer: at, Kind: InvalidType, Expected: "a value of type " + strings.Join(k.Want, " or ")})
	case *kind.PropertyNames:
		f.addPropertyNames(trusted, len(err.InstanceLocation), k.Property)
	default:
		f.add(Violation{Pointer: at, Kind: InvalidValue, Expected: expected(err.ErrorKind)})
	}
}

func (f *flattener) addFields(at string, fields []string, violationKind Kind) {
	for _, field := range fields {
		if f.truncated {
			return
		}
		f.add(Violation{Pointer: yamlvalue.Join(at, field), Field: field, Kind: violationKind})
	}
}

// addPropertyNames adds a violation for each object a propertyNames failure is about. The
// validator records that failure's location as a slice that later sibling validations overwrite
// (jsonschema/v6 validator.go:296 keeps vd.vloc where every other kind keeps a copy), so only its
// length is reliable: the objects are every one at that depth below the trusted ancestor holding
// the key. The walk carries tokens and joins a pointer only for a match.
func (f *flattener) addPropertyNames(trusted []string, depth int, property string) {
	node, ok := descend(f.instance, trusted)
	if !ok {
		return
	}
	var visit func(node any, tokens []string, remaining int)
	visit = func(node any, tokens []string, remaining int) {
		if f.truncated {
			return
		}
		switch container := node.(type) {
		case map[string]any:
			if remaining == 0 {
				if _, present := container[property]; present {
					f.add(Violation{Pointer: yamlvalue.Pointer(append(tokens, property)), Kind: InvalidValue, Expected: "a member name the schema allows"})
				}
				return
			}
			for _, key := range slices.Sorted(maps.Keys(container)) {
				visit(container[key], append(tokens, key), remaining-1)
			}
		case []any:
			for index, child := range container {
				visit(child, append(tokens, strconv.Itoa(index)), remaining-1)
			}
		}
	}
	// Clipped, so appending never writes into the validator's own location slice.
	visit(node, slices.Clip(trusted), depth-len(trusted))
}

// compareCauses orders sibling failures by schema location, then instance location, then the
// member name of a propertyNames failure, whose own instance location is not compared because it
// is unreliable.
func compareCauses(a, b *jsonschema.ValidationError) int {
	return cmp.Or(
		strings.Compare(a.SchemaURL, b.SchemaURL),
		slices.Compare(reliableLocation(a), reliableLocation(b)),
		strings.Compare(propertyName(a), propertyName(b)),
	)
}

func reliableLocation(err *jsonschema.ValidationError) []string {
	if _, isName := err.ErrorKind.(*kind.PropertyNames); isName {
		return nil
	}
	return err.InstanceLocation
}

func propertyName(err *jsonschema.ValidationError) string {
	if name, isName := err.ErrorKind.(*kind.PropertyNames); isName {
		return name.Property
	}
	return ""
}

func descend(node any, tokens []string) (any, bool) {
	for _, token := range tokens {
		switch container := node.(type) {
		case map[string]any:
			child, present := container[token]
			if !present {
				return nil, false
			}
			node = child
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(container) {
				return nil, false
			}
			node = container[index]
		default:
			return nil, false
		}
	}
	return node, true
}

// primaryCauses drops "unevaluated property" failures when a sibling failed too: a failed subschema
// keeps no record of the properties it knows, so every property it defines would also be reported
// as unknown, hiding the one real cause.
func primaryCauses(causes []*jsonschema.ValidationError) []*jsonschema.ValidationError {
	primary := make([]*jsonschema.ValidationError, 0, len(causes))
	for _, cause := range causes {
		if _, ok := cause.ErrorKind.(*kind.FalseSchema); ok && isUnevaluated(cause) {
			continue
		}
		primary = append(primary, cause)
	}
	if len(primary) == 0 {
		return causes
	}
	return primary
}

func isUnevaluated(err *jsonschema.ValidationError) bool {
	return strings.HasSuffix(err.SchemaURL, "/unevaluatedProperties")
}

func expected(errorKind jsonschema.ErrorKind) string {
	switch k := errorKind.(type) {
	case *kind.Const:
		return "the value the schema requires"
	case *kind.Enum:
		return "one of the values the schema lists"
	case *kind.Pattern:
		return fmt.Sprintf("text matching %q", k.Want)
	case *kind.Format:
		return "a valid " + k.Want
	case *kind.MinLength:
		return fmt.Sprintf("at least %d characters", k.Want)
	case *kind.MaxLength:
		return fmt.Sprintf("at most %d characters", k.Want)
	case *kind.Minimum:
		return "at least " + ratString(k.Want)
	case *kind.Maximum:
		return "at most " + ratString(k.Want)
	case *kind.ExclusiveMinimum:
		return "greater than " + ratString(k.Want)
	case *kind.ExclusiveMaximum:
		return "less than " + ratString(k.Want)
	case *kind.MultipleOf:
		return "a multiple of " + ratString(k.Want)
	case *kind.MinItems:
		return fmt.Sprintf("at least %d items", k.Want)
	case *kind.MaxItems:
		return fmt.Sprintf("at most %d items", k.Want)
	case *kind.MinProperties:
		return fmt.Sprintf("at least %d members", k.Want)
	case *kind.MaxProperties:
		return fmt.Sprintf("at most %d members", k.Want)
	case *kind.UniqueItems:
		return "items without duplicates"
	case *kind.Not:
		return "a value the schema does not exclude"
	default:
		return "a value the schema accepts"
	}
}

func ratString(value *big.Rat) string {
	if value == nil {
		return "the schema bound"
	}
	return value.RatString()
}

func pointerOf(tokens []string) string { return yamlvalue.Pointer(tokens) }

func lastToken(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

func dedupe(violations []Violation) []Violation {
	slices.SortFunc(violations, func(a, b Violation) int {
		if c := strings.Compare(a.Pointer, b.Pointer); c != 0 {
			return c
		}
		return int(a.Kind) - int(b.Kind)
	})
	return slices.CompactFunc(violations, func(a, b Violation) bool {
		return a.Pointer == b.Pointer && a.Kind == b.Kind
	})
}
