package validation

import (
	"fmt"
	"slices"

	"github.com/TomazVidovic533/stackpuddle/model"
)

// Result is the outcome of Validate.
type Result struct {
	// Diagnostics are the findings, sorted by file, line, column, pointer and code.
	Diagnostics model.Diagnostics
	// Unchecked names the definitions that were not checked because they are unresolved or are
	// packaged with no validator in the catalog; sorted, unique. A caller that provisions must
	// refuse a project while this is non-empty.
	Unchecked []model.ModuleName
}

// Validate checks every module instance of p, at project level and in each environment that
// overrides it: first the effective options against the definition's option schema, then, when that
// is clean and the catalog has the module, its semantic rules. It is pure: no I/O, clock or
// environment. The error reports a programming fault only (a validator emitted a code the model
// refuses, or options that passed the schema did not decode).
func Validate(p model.Project) (Result, error) {
	var result Result
	catalog := validators()
	for _, instance := range p.ModuleInstances() {
		definition, resolved := p.Definition(instance.Definition())
		if !resolved {
			result.Unchecked = append(result.Unchecked, instance.Definition())
			continue
		}
		// A project-local definition has no Go validator: the schema check alone applies to it.
		var check validator
		if definition.Origin() == model.OriginPackaged {
			var cataloged bool
			if check, cataloged = catalog[definition.Name().String()]; !cataloged {
				result.Unchecked = append(result.Unchecked, instance.Definition())
				continue
			}
		}
		found, err := validateInstance(p, instance.Key(), definition, check)
		if err != nil {
			return Result{}, err
		}
		result.Diagnostics = append(result.Diagnostics, found...)
	}
	result.Diagnostics = result.Diagnostics.Sorted()
	slices.SortFunc(result.Unchecked, func(a, b model.ModuleName) int { return compareNames(a, b) })
	result.Unchecked = slices.CompactFunc(result.Unchecked, func(a, b model.ModuleName) bool { return a == b })
	return result, nil
}

func compareNames(a, b model.ModuleName) int {
	switch {
	case a.String() < b.String():
		return -1
	case a.String() > b.String():
		return 1
	default:
		return 0
	}
}

// validateInstance checks one instance at project level, then in each environment that overrides
// it, dropping what the project level already reported so an override's own finding appears once.
func validateInstance(p model.Project, key model.ObjectKey, definition model.ModuleDefinition, check validator) (model.Diagnostics, error) {
	projectOptions, ok := p.EffectiveOptions(key)
	if !ok {
		return nil, fmt.Errorf("validate module/%s: effective options are unavailable for a resolved definition", key)
	}
	found, reported, err := checkOptions(projectOptions, definition, check, nil)
	if err != nil {
		return nil, err
	}
	for _, environment := range p.Environments() {
		if !overrides(environment, key) {
			continue
		}
		options, ok := p.EnvironmentOptions(environment.Name(), key)
		if !ok {
			return nil, fmt.Errorf("validate module/%s in env/%s: effective options are unavailable", key, environment.Name())
		}
		more, _, err := checkOptions(options, definition, check, reported)
		if err != nil {
			return nil, err
		}
		found = append(found, more...)
	}
	return found, nil
}

func overrides(environment model.Environment, key model.ObjectKey) bool {
	return slices.ContainsFunc(environment.ModuleOverrides(), func(override model.ModuleOverride) bool { return override.Key == key })
}

// finding identifies a diagnostic by its code and its options-relative pointer.
type finding struct {
	code    model.Code
	pointer string
}

// checkOptions runs the schema check and, when it is clean, the module's rules over options. It
// returns the diagnostics placed in their source files, minus those in skip, and the set of what it
// found before placement.
func checkOptions(options model.EffectiveOptions, definition model.ModuleDefinition, check validator, skip map[finding]bool) (model.Diagnostics, map[finding]bool, error) {
	found := definition.ValidateOptions(options.JSON())
	if len(found) == 0 && check != nil {
		var err error
		if found, err = check(options.JSON()); err != nil {
			return nil, nil, fmt.Errorf("validate module/%s: %w", options.Instance(), err)
		}
	}
	seen := map[finding]bool{}
	var placed model.Diagnostics
	for _, diagnostic := range found {
		relative := finding{code: diagnostic.Code(), pointer: diagnostic.Location().Pointer()}
		seen[relative] = true
		if skip[relative] {
			continue
		}
		placed = append(placed, diagnostic.At(options.Source(relative.pointer).Location))
	}
	return placed, seen, nil
}
