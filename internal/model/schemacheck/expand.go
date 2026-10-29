package schemacheck

import (
	"errors"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/model/yamlvalue"
)

// The reasons ExpandedValues refuses a schema.
var (
	// ErrUnsupportedReference is returned for a reference that is not "#/$defs/<name>" naming a
	// definition of the same schema, and for $dynamicRef, $anchor and the other keywords that
	// redirect a reference at validation time.
	ErrUnsupportedReference = errors.New("unsupported schema reference")
	// ErrReferenceCycle is returned when a definition reaches itself through references.
	ErrReferenceCycle = errors.New("schema references form a cycle")
	// ErrTooLarge is returned when the schema with its references expanded exceeds the limit.
	ErrTooLarge = errors.New("expanded schema is too large")
)

const (
	defsPrefix = "#/$defs/"
	defsKey    = "$defs"
	// Dialect is the only draft an option schema may use; the compiler is pinned to it too.
	Dialect = "https://json-schema.org/draft/2020-12/schema"
)

// redirecting are the keywords that make a reference resolve somewhere the static count cannot see.
var redirecting = map[string]bool{
	"$dynamicRef": true, "$dynamicAnchor": true, "$anchor": true, "$recursiveRef": true, "$recursiveAnchor": true,
}

// subschema, subschemas and subschemaMaps name the keywords whose value is a schema, a list of
// schemas and an object of schemas. Any other keyword holds plain data, whatever it contains.
var (
	subschema = map[string]bool{
		"additionalProperties": true, "additionalItems": true, "items": true, "contains": true, "not": true,
		"if": true, "then": true, "else": true, "propertyNames": true, "unevaluatedProperties": true,
		"unevaluatedItems": true, "contentSchema": true,
	}
	subschemas    = map[string]bool{"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true}
	subschemaMaps = map[string]bool{"properties": true, "patternProperties": true, "dependentSchemas": true, "dependencies": true}
)

// ExpandedValues returns how many values a validator may visit in schema when every "$ref" is
// replaced by the definition it names, counted once per use. Only "#/$defs/<name>" references of the
// schema's own top-level $defs are accepted. It returns ErrTooLarge once the count passes limit, so
// a schema whose expansion doubles with each level is refused after a bounded amount of work: the
// size of each definition is computed once.
func ExpandedValues(schema yamlvalue.Value, limit int) (int, error) {
	root, ok := schema.(map[string]any)
	if !ok {
		return 1, nil
	}
	defs, _ := root[defsKey].(map[string]any)
	e := &expander{defs: defs, limit: limit, sizes: map[string]int{}, active: map[string]bool{}}
	size, err := e.schema(root, true)
	if err != nil {
		return 0, err
	}
	if size > limit {
		return 0, ErrTooLarge
	}
	return size, nil
}

type expander struct {
	defs   map[string]any
	limit  int
	sizes  map[string]int
	active map[string]bool
}

// add saturates one past the limit, so sizes never overflow.
func (e *expander) add(a, b int) int { return min(a+b, e.limit+1) }

// schema counts one schema value. top skips the root's own $defs, which count only where used.
func (e *expander) schema(node any, top bool) (int, error) {
	object, ok := node.(map[string]any)
	if !ok {
		return e.data(node), nil
	}
	total := 1
	for key, child := range object {
		var size int
		var err error
		switch {
		case key == defsKey || key == "definitions":
			if !top && key == defsKey {
				return 0, ErrUnsupportedReference
			}
			continue
		case redirecting[key], key == "$id" && !top:
			// A nested $id would start a new resource, so a reference below it would not mean what it says.
			return 0, ErrUnsupportedReference
		case key == "$schema":
			// Another draft reads keywords differently (a list-valued items holds subschemas), so
			// the count would not match what the validator runs.
			if text, isText := child.(string); !isText || (text != Dialect && text != Dialect+"#") {
				return 0, ErrUnsupportedReference
			}
			size = 1
		case (key == "items" || key == "additionalItems") && isList(child):
			// A tuple form belongs to older drafts; 2020-12 uses prefixItems.
			return 0, ErrUnsupportedReference
		case key == "$ref":
			size, err = e.reference(child)
		case subschema[key]:
			size, err = e.schema(child, false)
		case subschemas[key]:
			size, err = e.each(child, false)
		case subschemaMaps[key]:
			size, err = e.each(child, true)
		default:
			size = e.data(child)
		}
		if err != nil {
			return 0, err
		}
		total = e.add(total, size)
	}
	return total, nil
}

// each counts a list of schemas, or the members of an object of schemas.
func (e *expander) each(node any, members bool) (int, error) {
	total := 1
	var children []any
	switch container := node.(type) {
	case []any:
		children = container
	case map[string]any:
		if !members {
			return e.data(node), nil
		}
		for _, child := range container {
			children = append(children, child)
		}
	default:
		return e.data(node), nil
	}
	for _, child := range children {
		size, err := e.schema(child, false)
		if err != nil {
			return 0, err
		}
		total = e.add(total, size)
	}
	return total, nil
}

// reference counts the value of the "$ref" keyword itself plus the definition it names.
func (e *expander) reference(node any) (int, error) {
	text, ok := node.(string)
	if !ok {
		return 1, nil
	}
	name, ok := strings.CutPrefix(text, defsPrefix)
	// A percent-encoded name is decoded by the validator, so the literal lookup below would count a decoy.
	if !ok || name == "" || strings.ContainsAny(name, "/%") {
		return 0, ErrUnsupportedReference
	}
	name = strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
	if size, done := e.sizes[name]; done {
		return e.add(1, size), nil
	}
	target, exists := e.defs[name]
	if !exists {
		return 0, ErrUnsupportedReference
	}
	if e.active[name] {
		return 0, ErrReferenceCycle
	}
	e.active[name] = true
	size, err := e.schema(target, false)
	delete(e.active, name)
	if err != nil {
		return 0, err
	}
	e.sizes[name] = size
	return e.add(1, size), nil
}

func isList(node any) bool {
	_, ok := node.([]any)
	return ok
}

// data counts the values of a keyword that holds no schema, saturating at the limit.
func (e *expander) data(node any) int {
	total := 1
	switch container := node.(type) {
	case map[string]any:
		for _, child := range container {
			total = e.add(total, e.data(child))
		}
	case []any:
		for _, child := range container {
			total = e.add(total, e.data(child))
		}
	}
	return total
}
