package model

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

// OptionLayer is the configuration layer that supplied an option value, lowest precedence first.
type OptionLayer int

const (
	// OptionLayerDefault is the definition's defaults file.
	OptionLayerDefault OptionLayer = iota + 1
	// OptionLayerProject is the instance's declared options.
	OptionLayerProject
	// OptionLayerEnvironment is the selected environment's override of the instance.
	OptionLayerEnvironment
)

// String returns "default", "project" or "environment".
func (l OptionLayer) String() string {
	switch l {
	case OptionLayerDefault:
		return "default"
	case OptionLayerProject:
		return "project"
	case OptionLayerEnvironment:
		return "environment"
	default:
		return "unknown"
	}
}

// OptionSource names where one effective option value came from.
type OptionSource struct {
	// Layer is the layer whose document supplied the value.
	Layer OptionLayer
	// Location is the file and pointer in that layer's document, with line and column when the
	// parsed document has them.
	Location Location
}

// EffectiveOptions is an instance's options after layering the definition's defaults, the declared
// options and, for EnvironmentOptions, the environment's override. It is immutable and safe for
// concurrent use. Its values are not validated: pass JSON to ModuleDefinition.ValidateOptions.
type EffectiveOptions struct {
	instance    ObjectKey
	definition  ModuleName
	environment optional[EnvironmentName]
	json        []byte
	sources     map[string]optionOrigin
	fallback    optionOrigin
}

// optionOrigin is the layer and the document root a value's source pointer is relative to.
type optionOrigin struct {
	layer OptionLayer
	at    origin
}

// Instance returns the key of the module instance the options belong to.
func (o EffectiveOptions) Instance() ObjectKey { return o.instance }

// Definition returns the name of the instance's Module Definition.
func (o EffectiveOptions) Definition() ModuleName { return o.definition }

// Environment returns the environment whose override was applied. ok is false for project-level
// options.
func (o EffectiveOptions) Environment() (EnvironmentName, bool) { return o.environment.get() }

// JSON returns the canonical JSON bytes of the options. Each call returns a copy.
func (o EffectiveOptions) JSON() json.RawMessage { return slices.Clone(o.json) }

// Source returns where the value at pointer, a JSON pointer into JSON, came from. A pointer with no
// value of its own resolves to the nearest ancestor that has one, so a violation of an absent
// member is placed at the object that should have declared it.
func (o EffectiveOptions) Source(pointer string) OptionSource {
	for {
		if found, ok := o.sources[pointer]; ok {
			return OptionSource{Layer: found.layer, Location: found.at.location(pointer)}
		}
		cut := strings.LastIndex(pointer, "/")
		if cut < 0 {
			return OptionSource{Layer: o.fallback.layer, Location: o.fallback.at.location("")}
		}
		pointer = pointer[:cut]
	}
}

// EffectiveOptions returns the options of instance key: the definition's defaults with the declared
// options merged on top. Objects merge by key; scalars and lists replace. ok is false when the
// project has no such instance or its definition is unresolved (see Unresolved).
func (p Project) EffectiveOptions(key ObjectKey) (EffectiveOptions, bool) {
	instance, tree, recorder, ok := p.layeredOptions(key)
	if !ok {
		return EffectiveOptions{}, false
	}
	return recorder.finish(instance, optional[EnvironmentName]{}, tree)
}

// EnvironmentOptions returns EffectiveOptions(key) with environment env's override of key merged on
// top. ok is false for an environment the project does not declare, and in the cases of
// EffectiveOptions. Without an override the options equal the project level, with Environment set.
func (p Project) EnvironmentOptions(env EnvironmentName, key ObjectKey) (EffectiveOptions, bool) {
	environment, declared := p.Environment(env)
	if !declared {
		return EffectiveOptions{}, false
	}
	instance, tree, recorder, ok := p.layeredOptions(key)
	if !ok {
		return EffectiveOptions{}, false
	}
	for _, override := range environment.moduleOverrides {
		if override.Key != key {
			continue
		}
		recorder.layer, recorder.at = OptionLayerEnvironment, environment.moduleOverrideAt[key]
		tree = overlayOptions(tree, optionsTree(recorder.at), "", recorder)
	}
	return recorder.finish(instance, some(env), tree)
}

// layeredOptions merges the defaults and the declared options of instance key, recording sources.
func (p Project) layeredOptions(key ObjectKey) (ModuleInstance, any, *optionRecorder, bool) {
	instance, found := p.module(key)
	if !found {
		return ModuleInstance{}, nil, nil, false
	}
	definition, resolved := p.Definition(instance.definition)
	if !resolved {
		return ModuleInstance{}, nil, nil, false
	}
	recorder := &optionRecorder{sources: map[string]optionOrigin{}}
	var tree any = map[string]any{}
	if definition.defaultsAt.doc != nil {
		recorder.layer, recorder.at = OptionLayerDefault, definition.defaultsAt
		tree = overlayOptions(tree, definition.defaultsAt.doc.tree, "", recorder)
	}
	recorder.layer, recorder.at = OptionLayerProject, instance.optionsAt
	tree = overlayOptions(tree, optionsTree(instance.optionsAt), "", recorder)
	return instance, tree, recorder, true
}

// optionRecorder notes, for each pointer of the merged options, the layer that last set it.
type optionRecorder struct {
	layer   OptionLayer
	at      origin
	sources map[string]optionOrigin
}

func (r *optionRecorder) finish(instance ModuleInstance, environment optional[EnvironmentName], tree any) (EffectiveOptions, bool) {
	value, err := canonical.FromTree(tree)
	if err != nil {
		return EffectiveOptions{}, false
	}
	return EffectiveOptions{
		instance:    instance.key,
		definition:  instance.definition,
		environment: environment,
		json:        canonical.Encode(value),
		sources:     r.sources,
		fallback:    optionOrigin{layer: OptionLayerProject, at: instance.optionsAt},
	}, true
}

// note records pointer as set by the current layer.
func (r *optionRecorder) note(pointer string) {
	if r != nil {
		r.sources[pointer] = optionOrigin{layer: r.layer, at: r.at}
	}
}

// replace records value, which replaced base at pointer, and every member of it. Only the members of
// base are forgotten, so the cost follows the two subtrees and not the whole recorded map.
func (r *optionRecorder) replace(pointer string, base, value any) {
	if r == nil {
		return
	}
	r.forget(pointer, base)
	r.record(pointer, value)
}

// forget drops the record of pointer and of every member of value, the tree that was at pointer.
func (r *optionRecorder) forget(pointer string, value any) {
	delete(r.sources, pointer)
	switch container := value.(type) {
	case map[string]any:
		for key, member := range container {
			r.forget(joinPointer(pointer, key), member)
		}
	case []any:
		for index, member := range container {
			r.forget(joinPointer(pointer, strconv.Itoa(index)), member)
		}
	}
}

func (r *optionRecorder) record(pointer string, value any) {
	r.note(pointer)
	switch container := value.(type) {
	case map[string]any:
		for key, member := range container {
			r.record(joinPointer(pointer, key), member)
		}
	case []any:
		for index, member := range container {
			r.record(joinPointer(pointer, strconv.Itoa(index)), member)
		}
	}
}

// overlayOptions applies override on base: objects merge by key, scalars and lists replace. It is the
// one implementation of the rule, shared by the load-time checks (recorder nil) and EffectiveOptions.
func overlayOptions(base, override any, pointer string, recorder *optionRecorder) any {
	baseObject, baseIsObject := base.(map[string]any)
	overrideObject, overrideIsObject := override.(map[string]any)
	if !baseIsObject || !overrideIsObject {
		recorder.replace(pointer, base, override)
		return override
	}
	merged := make(map[string]any, len(baseObject)+len(overrideObject))
	for key, value := range baseObject {
		merged[key] = value
	}
	for key, value := range overrideObject {
		merged[key] = overlayOptions(baseObject[key], value, joinPointer(pointer, key), recorder)
	}
	recorder.note(pointer)
	return merged
}
