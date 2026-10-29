package validation

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/TomazVidovic533/stackpuddle/model"
	"github.com/TomazVidovic533/stackpuddle/validation/postgres"
)

// validator checks the effective options of one module instance that already satisfy the module's
// option schema. Its diagnostics carry pointers relative to the options.
type validator func(options json.RawMessage) (model.Diagnostics, error)

// validators is the catalog: one line per module, in one place. A new module adds a line here and a
// package beside postgres/. It is a function, not a package variable, so nothing can change it.
func validators() map[string]validator {
	return map[string]validator{
		"postgres": postgres.Validate,
	}
}

// Modules returns the names of the modules that have a semantic validator, sorted.
func Modules() []model.ModuleName {
	catalog := validators()
	names := make([]model.ModuleName, 0, len(catalog))
	for _, text := range slices.Sorted(maps.Keys(catalog)) {
		name, err := model.ParseModuleName(text)
		if err != nil {
			panic("validation: catalog key " + text + " is not a module name: " + err.Error())
		}
		names = append(names, name)
	}
	return names
}
