package model

import (
	"encoding/json"
	"slices"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
)

// DefinitionOrigin says where a Module Definition came from.
type DefinitionOrigin int

const (
	// OriginPackaged is a definition from a module package the caller supplied.
	OriginPackaged DefinitionOrigin = iota + 1
	// OriginLocal is a definition declared inside the project, with no publication step.
	OriginLocal
)

// String returns "packaged" or "local".
func (o DefinitionOrigin) String() string {
	switch o {
	case OriginPackaged:
		return "packaged"
	case OriginLocal:
		return "local"
	default:
		return "unknown"
	}
}

// ModuleDefinition is the generic contract every module uses, packaged or project-local: the
// capabilities it needs, the data format it keeps, its option schema, defaults and the workloads it
// contributes. It is immutable and safe for concurrent use.
type ModuleDefinition struct {
	name       ModuleName
	version    Version
	dataFormat optional[DataFormat]
	requires   []Capability
	origin     DefinitionOrigin
	workloads  []Workload
	options    *schemacheck.Set
	// optionCost is the option schema with its references expanded, in values; 1 without a schema.
	optionCost int
	defaults   optional[[]byte]
	// defaultsAt is the defaults file's document, so EffectiveOptions can say where a default sits.
	defaultsAt origin
	descriptor *document
}

// Name returns the definition's name.
func (d ModuleDefinition) Name() ModuleName { return d.name }

// Version returns the definition's version.
func (d ModuleDefinition) Version() Version { return d.version }

// DataFormat returns the format of the data the module keeps, if it keeps any.
func (d ModuleDefinition) DataFormat() (DataFormat, bool) { return d.dataFormat.get() }

// Requires returns the capabilities an engine must advertise to run the module, sorted.
func (d ModuleDefinition) Requires() []Capability { return slices.Clone(d.requires) }

// Origin returns whether the definition is packaged or project-local.
func (d ModuleDefinition) Origin() DefinitionOrigin { return d.origin }

// Workloads returns the workload templates the definition contributes, ordered by key.
func (d ModuleDefinition) Workloads() []Workload { return slices.Clone(d.workloads) }

// DefinitionExport is an Export of a definition and the contributed workload that offers it.
type DefinitionExport struct {
	Workload ObjectKey
	Export   Export
}

// Exports returns every Export of the contributed workloads, ordered by key. Export keys are unique
// within a definition, so an instance's Export is named by its key alone.
func (d ModuleDefinition) Exports() []DefinitionExport {
	var exports []DefinitionExport
	for _, workload := range d.workloads {
		for _, export := range workload.Exports() {
			exports = append(exports, DefinitionExport{Workload: workload.key, Export: export})
		}
	}
	slices.SortFunc(exports, func(a, b DefinitionExport) int {
		return compareText(a.Export.Key.String(), b.Export.Key.String())
	})
	return exports
}

// Dependencies returns every Dependency of the contributed workloads, ordered by key. Dependency
// keys are unique within a definition.
func (d ModuleDefinition) Dependencies() []Dependency {
	var dependencies []Dependency
	for _, workload := range d.workloads {
		dependencies = append(dependencies, workload.dependencies...)
	}
	slices.SortFunc(dependencies, func(a, b Dependency) int { return compareText(a.Key.String(), b.Key.String()) })
	return dependencies
}

// Defaults returns the canonical JSON bytes of the definition's default options, if it has any.
func (d ModuleDefinition) Defaults() (json.RawMessage, bool) {
	defaults, ok := d.defaults.get()
	return slices.Clone(defaults), ok
}

// ValidateOptions checks options, a JSON object such as an instance's effective options, against
// the definition's option schema. One call is charged like a Load check and refused with
// syntax.limit_exceeded past the same budget. Diagnostic pointers are relative to options and carry no file or
// position; a caller maps them to its own source. A definition without an option schema accepts
// only an empty object. At most 100 problems are reported; past that, one syntax.limit_exceeded
// says the report was cut.
func (d ModuleDefinition) ValidateOptions(options json.RawMessage) Diagnostics {
	value, err := canonical.Parse(options)
	if err != nil {
		return Diagnostics{newDiagnostic(StructureInvalidType, NewLocation("", "", 0, 0), "options must be one JSON object")}
	}
	tree := treeOf(value)
	if d.optionCheckCost(tree) > maxOptionCheckUnits {
		return Diagnostics{newDiagnostic(SyntaxLimitExceeded, NewLocation("", "", 0, 0), optionCheckLimitMessage())}
	}
	return violationDiagnostics(d.checkOptions(tree), origin{})
}

// optionCheckCost is what checking tree against the option schema is charged: its values times the
// schema's expanded size.
func (d ModuleDefinition) optionCheckCost(tree any) int {
	return countValues(tree) * max(d.optionCost, 1)
}

const optionSchemaID = "urn:stackpuddle:module-options"

// checkOptions validates an options tree, keeping at most maxFileDiagnostics violations.
func (d ModuleDefinition) checkOptions(tree any) schemacheck.Result {
	object, isObject := tree.(map[string]any)
	if !isObject {
		return schemacheck.Result{Violations: []schemacheck.Violation{{Kind: schemacheck.InvalidType, Expected: "a value of type object"}}}
	}
	if d.options == nil {
		fields := sortedKeys(object)
		kept := fields[:min(len(fields), maxFileDiagnostics)]
		result := schemacheck.Result{Truncated: len(fields) > len(kept)}
		for _, field := range kept {
			result.Violations = append(result.Violations, schemacheck.Violation{Pointer: joinPointer("", field), Field: field, Kind: schemacheck.UnknownField})
		}
		return result
	}
	result, err := d.options.Check(optionSchemaID, tree, maxFileDiagnostics)
	if err != nil {
		return schemacheck.Result{Violations: []schemacheck.Violation{{Kind: schemacheck.InvalidValue, Expected: "options the definition's schema accepts"}}}
	}
	return result
}

func (d ModuleDefinition) workload(key ObjectKey) (Workload, bool) {
	for _, workload := range d.workloads {
		if workload.key == key {
			return workload, true
		}
	}
	return Workload{}, false
}

func (d ModuleDefinition) export(key ExportKey) (DefinitionExport, bool) {
	for _, export := range d.Exports() {
		if export.Export.Key == key {
			return export, true
		}
	}
	return DefinitionExport{}, false
}

// dependencyWorkload returns the contributed workload that declares the Dependency key.
func (d ModuleDefinition) dependencyWorkload(key DependencyKey) (Workload, Dependency, bool) {
	for _, workload := range d.workloads {
		if dependency, ok := workload.dependency(key); ok {
			return workload, dependency, true
		}
	}
	return Workload{}, Dependency{}, false
}

// ModuleInstance is one declared use of a Module Definition with its options.
type ModuleInstance struct {
	key         ObjectKey
	definition  ModuleName
	options     []byte
	description optional[string]
	optionsAt   origin
}

// Key returns the instance's key.
func (m ModuleInstance) Key() ObjectKey { return m.key }

// Definition returns the name of the definition the instance uses.
func (m ModuleInstance) Definition() ModuleName { return m.definition }

// Options returns the canonical JSON bytes of the declared options; "{}" and a newline when none.
// Values are as written: defaults and environment overrides are not applied.
func (m ModuleInstance) Options() json.RawMessage { return slices.Clone(m.options) }

// Locate returns the source location of pointer, a JSON pointer into Options, so a validator can
// report against the declaration. An absent member resolves to its nearest declared ancestor.
func (m ModuleInstance) Locate(pointer string) Location {
	return m.optionsAt.location(pointer)
}

func (m ModuleInstance) ref() ObjectRef { return ObjectRef{kind: ObjectModule, key: m.key} }
