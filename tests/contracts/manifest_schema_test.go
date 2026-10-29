package contracts_test

import "testing"

func TestManifestSchema_AcceptsValidFixture(t *testing.T) {
	t.Parallel()
	assertValidFixture(t, "manifest", "valid/manifest.yaml")
}

func TestManifestSchema_RejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		location string
		keyword  string
	}{
		{name: "resolved digest", fixture: "invalid/manifest-resolved-digest.yaml", location: "/bundleDigest", keyword: "additionalProperties"},
		{name: "duplicate module", fixture: "invalid/manifest-duplicate-module.yaml", location: "/modules", keyword: "uniqueItems"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertInvalidFixture(t, "manifest", test.fixture, test.location, test.keyword)
		})
	}
}

func TestManifestSchema_RequiresEveryField(t *testing.T) {
	t.Parallel()

	tests := []requiredFieldCase{
		{name: "apiVersion", path: []string{"apiVersion"}},
		{name: "kind", path: []string{"kind"}},
		{name: "bundleVersion", path: []string{"bundleVersion"}},
		{name: "modules", path: []string{"modules"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := loadContractFixture(t, "valid/manifest.yaml")
			removeContractField(t, document, test.path...)

			err := validateContract(t, "manifest", document)

			if err == nil {
				t.Fatal("expected missing required field to fail validation")
			}
			assertRequiredFailure(t, err, parentLocation(test.path), test.path[len(test.path)-1])
		})
	}
}

func TestManifestSchema_EnforcesContractBoundaries(t *testing.T) {
	t.Parallel()

	tests := []schemaRejectionCase{
		{name: "unsupported API version", path: []string{"apiVersion"}, value: "stackpuddle/v2", location: "/apiVersion", keyword: "const"},
		{name: "wrong kind", path: []string{"kind"}, value: "Lock", location: "/kind", keyword: "const"},
		{name: "null bundle version", path: []string{"bundleVersion"}, value: nil, location: "/bundleVersion", keyword: "type"},
		{name: "empty module selection", path: []string{"modules"}, value: []any{}, location: "/modules", keyword: "minItems"},
		{name: "uppercase module name", path: []string{"modules"}, value: []any{"Postgres"}, location: "/modules/0", keyword: "pattern"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := loadContractFixture(t, "valid/manifest.yaml")
			setContractField(t, document, test.value, test.path...)

			err := validateContract(t, "manifest", document)

			if err == nil {
				t.Fatal("expected contract validation to fail")
			}
			assertValidationFailure(t, err, test.location, test.keyword)
		})
	}
}

func TestManifestSchema_AcceptsAnyModuleOrder(t *testing.T) {
	t.Parallel()

	document := loadContractFixture(t, "valid/manifest.yaml")
	setContractField(t, document, []any{"postgres", "observability"}, "modules")

	err := validateContract(t, "manifest", document)

	if err != nil {
		t.Fatalf("expected reordered modules to remain valid: %v", err)
	}
}
