package model_test

import (
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/model"
)

func validVersionInfo() model.VersionInfo {
	return model.NewVersionInfo(
		model.CLIBuild{Version: "v0.1.0", Go: "go1.25.0", Platform: "linux/amd64"},
		model.ModelBuild{Version: "v0.1.0", APIVersions: []string{"stackpuddle/v1"}},
	)
}

// TestEncodeVersionInfoRoundTrips: what it sends is what ParseVersionInfo accepts.
func TestEncodeVersionInfoRoundTrips(t *testing.T) {
	t.Parallel()
	data, err := model.EncodeVersionInfo(validVersionInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, diagnostics := model.ParseVersionInfo(data); len(diagnostics) > 0 {
		t.Fatalf("the encoded document does not parse: %v", diagnostics[0])
	}
}

// TestEncodeVersionInfoRefusesWhatTheDecoderRefuses is the failure path: a document the receiver
// would reject is never returned.
func TestEncodeVersionInfoRefusesWhatTheDecoderRefuses(t *testing.T) {
	t.Parallel()
	engine := model.EngineInfo{Version: "v0.1.0", StateRead: []int{1}, StateWrite: 1, StoreRead: []int{1}, StoreWrite: 1}
	tests := map[string]model.VersionInfo{
		"the zero value":                     {},
		"an empty CLI version":               model.NewVersionInfo(model.CLIBuild{Go: "go1.25.0", Platform: "linux/amd64"}, model.ModelBuild{Version: "v0.1.0", APIVersions: []string{"stackpuddle/v1"}}),
		"a model version that is not semver": model.NewVersionInfo(model.CLIBuild{Version: "v0.1.0", Go: "go1.25.0", Platform: "linux/amd64"}, model.ModelBuild{Version: "latest", APIVersions: []string{"stackpuddle/v1"}}),
		"an engine with a malformed capability": validVersionInfo().WithEngine(model.EngineInfo{
			Version: "v0.1.0", Capabilities: []model.Capability{{}}, StateRead: engine.StateRead, StateWrite: 1, StoreRead: engine.StoreRead, StoreWrite: 1}),
	}
	for name, info := range tests {
		t.Run(name, func(t *testing.T) {
			data, err := model.EncodeVersionInfo(info)
			if err == nil || data != nil {
				t.Fatalf("got %q, %v; want no document and an error", data, err)
			}
			if !strings.Contains(err.Error(), "version information is not valid") {
				t.Errorf("error %q does not say what was refused", err)
			}
		})
	}
}
