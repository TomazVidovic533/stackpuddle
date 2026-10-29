package model_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

func TestParseRefRoundTripsEveryKind(t *testing.T) {
	t.Parallel()
	tests := map[string]model.RefKind{
		"workload/api":                            model.RefWorkload,
		"module/primary":                          model.RefModule,
		"client/cli":                              model.RefClient,
		"app/storefront":                          model.RefApp,
		"env/local":                               model.RefEnv,
		"resource/primary/database/catalog":       model.RefResource,
		"resource/primary/schema-grant/app_1":     model.RefResource,
		"operation/op-01JBZ8X3QK7M2N4P5R6S7T8V9W": model.RefOperation,
		"binding/workload/api/database":           model.RefBinding,
		"binding/module/primary/upstream":         model.RefBinding,
		"binding/client/cli/api":                  model.RefBinding,
	}
	for text, kind := range tests {
		ref, err := model.ParseRef(text)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", text, err)
			continue
		}
		if ref.Kind() != kind || ref.String() != text {
			t.Errorf("ParseRef(%q) = %s %q, want %s %q", text, ref.Kind(), ref.String(), kind, text)
		}
	}
}

func TestParseRefRefusesMalformedReferences(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"", "api", "workload/", "workload/API", "workload/api/extra", "service/api", "env/local.compose",
		"resource/primary/database", "resource/primary/Database/catalog", "resource/primary/database/Catalog",
		"resource/primary/database/" + strings.Repeat("a", 64),
		"operation/123", "operation/op-01JBZ8X3QK7M2N4P5R6S7T8V9", "operation/op-01JBZ8X3QK7M2N4P5R6S7T8V9I",
		"operation/op-01jbz8x3qk7m2n4p5r6s7t8v9w",
		"binding/workload/api", "binding/app/storefront/api", "binding/workload/api/database/extra",
	} {
		_, err := model.ParseRef(text)
		modelError, ok := errors.AsType[*model.Error](err)
		if !ok || modelError.Code() != model.ReferenceInvalidRef {
			t.Errorf("ParseRef(%q) = %v, want a reference.invalid_ref *model.Error", text, err)
		}
	}
}

func TestBindingReferenceEndsWithTheDependencyKey(t *testing.T) {
	t.Parallel()
	consumer, err := model.ParseObjectRef("workload/api")
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := model.ParseDependencyKey("database")
	if err != nil {
		t.Fatal(err)
	}
	ref := model.BindingRef{Consumer: consumer, Dependency: dependency}
	if ref.String() != "binding/workload/api/database" {
		t.Fatalf("BindingRef.String() = %q", ref.String())
	}
	parsed, err := model.ParseRef(ref.String())
	if err != nil || parsed.Kind() != model.RefBinding {
		t.Fatalf("the binding reference spelling does not parse back: %v", err)
	}
}

func TestParseObjectRefNamesKindAndKey(t *testing.T) {
	t.Parallel()
	ref, err := model.ParseObjectRef("client/cli")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind() != model.ObjectClient || ref.Key().String() != "cli" || ref.String() != "client/cli" {
		t.Fatalf("ParseObjectRef(client/cli) = %s %s %s", ref.Kind(), ref.Key(), ref)
	}
	if _, err := model.ParseObjectRef("app/storefront"); err == nil {
		t.Fatal("an application is not an object reference")
	}
}

// TestNewModuleDiagnosticKeepsModuleCodesApartFromCoreCodes protects the core code space: a module
// validator reports "<module>.<rule>" and can never impersonate a core stage.
func TestNewModuleDiagnosticKeepsModuleCodesApartFromCoreCodes(t *testing.T) {
	t.Parallel()
	at := model.NewLocation("stackpuddle.yaml", "/spec/modules/primary/options/users/app", 12, 11)
	diagnostic, err := model.NewModuleDiagnostic("postgres.shared_login_role", at, "a login role is shared")
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic.Code().Stage() != model.StageModule || diagnostic.Location() != at {
		t.Fatalf("got %s in stage %s at %+v", diagnostic.Code(), diagnostic.Code().Stage(), diagnostic.Location())
	}
	for _, code := range []model.Code{"structure.invalid_value", "binding.custom", "postgres", "Postgres.rule", "postgres.Rule", "postgres..rule"} {
		_, err := model.NewModuleDiagnostic(code, at, "message")
		if modelError, ok := errors.AsType[*model.Error](err); !ok || modelError.Code() != model.StructureInvalidValue {
			t.Errorf("NewModuleDiagnostic(%q) = %v, want a refusal", code, err)
		}
	}
}
