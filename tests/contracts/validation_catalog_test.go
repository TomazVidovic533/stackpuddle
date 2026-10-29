package contracts_test

import (
	"slices"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/validation"
)

// TestValidationCatalogMatchesModulesBothWays fails by name for a module under modules/ that has no
// semantic validator and for a validator that has no module (EN7).
func TestValidationCatalogMatchesModulesBothWays(t *testing.T) {
	t.Parallel()
	modules := repositoryModuleNames(t)
	var validators []string
	for _, name := range validation.Modules() {
		validators = append(validators, name.String())
	}
	for _, module := range modules {
		if !slices.Contains(validators, module) {
			t.Errorf("module %q under modules/ has no validator in validation.Modules(); add one line to the catalog in validation/catalog.go", module)
		}
	}
	for _, validator := range validators {
		if !slices.Contains(modules, validator) {
			t.Errorf("validator %q in validation.Modules() has no module under modules/", validator)
		}
	}
}

func TestValidationCatalogIsSortedAndUnique(t *testing.T) {
	t.Parallel()
	var names []string
	for _, name := range validation.Modules() {
		names = append(names, name.String())
	}
	if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("validation.Modules() = %v; want sorted and unique", names)
	}
}
