package contracts_test

import "testing"

func TestLockSchema_AcceptsValidFixture(t *testing.T) {
	t.Parallel()
	assertValidFixture(t, "lock", "valid/lock.yaml")
}

func TestLockSchema_RejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	t.Run("missing module digest", func(t *testing.T) {
		t.Parallel()
		assertFixtureMissingRequiredField(t, "lock", "invalid/lock-missing-module-digest.yaml", "/modules/postgres", "digest")
	})

	t.Run("floating module version", func(t *testing.T) {
		t.Parallel()
		assertInvalidFixture(t, "lock", "invalid/lock-floating-module-version.yaml", "/modules/postgres/version", "format")
	})
}

func TestLockSchema_RequiresEveryField(t *testing.T) {
	t.Parallel()

	tests := []requiredFieldCase{
		{name: "apiVersion", path: []string{"apiVersion"}},
		{name: "kind", path: []string{"kind"}},
		{name: "bundle", path: []string{"bundle"}},
		{name: "modules", path: []string{"modules"}},
		{name: "bundle version", path: []string{"bundle", "version"}},
		{name: "bundle source", path: []string{"bundle", "source"}},
		{name: "bundle digest", path: []string{"bundle", "digest"}},
		{name: "module version", path: []string{"modules", "postgres", "version"}},
		{name: "module source", path: []string{"modules", "postgres", "source"}},
		{name: "module format", path: []string{"modules", "postgres", "format"}},
		{name: "module digest", path: []string{"modules", "postgres", "digest"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := loadContractFixture(t, "valid/lock.yaml")
			removeContractField(t, document, test.path...)

			err := validateContract(t, "lock", document)

			if err == nil {
				t.Fatal("expected missing required field to fail validation")
			}
			assertRequiredFailure(t, err, parentLocation(test.path), test.path[len(test.path)-1])
		})
	}
}

func TestLockSchema_EnforcesContractBoundaries(t *testing.T) {
	t.Parallel()

	tests := []schemaRejectionCase{
		{name: "empty modules", path: []string{"modules"}, value: map[string]any{}, location: "/modules", keyword: "minProperties"},
		{name: "uppercase bundle digest", path: []string{"bundle", "digest"}, value: "sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", location: "/bundle/digest", keyword: "pattern"},
		{name: "HTTP remote source", path: []string{"bundle", "source"}, value: "http://artifacts.example.test/platform/bundle.tar.gz", location: "/bundle/source", keyword: "anyOf"},
		{name: "source user information", path: []string{"bundle", "source"}, value: "https://user@artifacts.example.test/platform/bundle.tar.gz", location: "/bundle/source", keyword: "anyOf"},
		{name: "source query", path: []string{"bundle", "source"}, value: "https://artifacts.example.test/platform/bundle.tar.gz?token=value", location: "/bundle/source", keyword: "anyOf"},
		{name: "source fragment", path: []string{"bundle", "source"}, value: "https://artifacts.example.test/platform/bundle.tar.gz#fragment", location: "/bundle/source", keyword: "anyOf"},
		{name: "unknown lock field", path: []string{"generatedAt"}, value: "2026-09-11T00:00:00Z", location: "/generatedAt", keyword: "additionalProperties"},
		{name: "unknown bundle field", path: []string{"bundle", "channel"}, value: "stable", location: "/bundle/channel", keyword: "additionalProperties"},
		{name: "unknown module artifact field", path: []string{"modules", "postgres", "revision"}, value: "main", location: "/modules/postgres/revision", keyword: "additionalProperties"},
		{name: "semantic version range", path: []string{"modules", "postgres", "version"}, value: "^0.1.0", location: "/modules/postgres/version", keyword: "format"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document := loadContractFixture(t, "valid/lock.yaml")
			setContractField(t, document, test.value, test.path...)

			err := validateContract(t, "lock", document)

			if err == nil {
				t.Fatal("expected contract validation to fail")
			}
			assertValidationFailure(t, err, test.location, test.keyword)
		})
	}
}

func TestLockSchema_AcceptsYAMLMappingOrder(t *testing.T) {
	t.Parallel()

	document := readFixtureYAML(t, []byte(`
modules:
  postgres:
    digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    format: tar+gzip
    source: https://artifacts.example.test/platform/postgres.tar.gz
    version: 0.1.0-alpha.1
bundle:
  digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  source: https://artifacts.example.test/platform/bundle.tar.gz
  version: 0.1.0-alpha.1
kind: Lock
apiVersion: stackpuddle/v1
`))

	err := compileSchema(t, "lock").Validate(document)

	if err != nil {
		t.Fatalf("expected reordered lock mapping to remain valid: %v", err)
	}
}
