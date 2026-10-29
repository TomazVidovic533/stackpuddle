package model

import (
	"slices"
	"strconv"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

// CLIBuild describes the command-line build: its module version as Go reports it, the Go toolchain
// and the GOOS/GOARCH platform.
type CLIBuild struct {
	Version  string
	Go       string
	Platform string
}

// ModelBuild describes the model library a build links: its module version and the API versions
// it reads.
type ModelBuild struct {
	Version     string
	APIVersions []string
}

// EngineInfo describes a linked engine: its version, the capabilities it advertises and the state
// and store versions it reads and writes.
type EngineInfo struct {
	Version      string
	Capabilities []Capability
	StateRead    []int
	StateWrite   int
	StoreRead    []int
	StoreWrite   int
}

// VersionInfo is what a build is and which contract versions it reads, as the version command
// prints it.
type VersionInfo struct {
	cli    CLIBuild
	model  ModelBuild
	engine optional[EngineInfo]
}

// NewVersionInfo returns a VersionInfo without engine information. It copies its inputs.
func NewVersionInfo(cli CLIBuild, model ModelBuild) VersionInfo {
	model.APIVersions = slices.Clone(model.APIVersions)
	return VersionInfo{cli: cli, model: model}
}

// WithEngine returns a copy of v that also describes a linked engine.
func (v VersionInfo) WithEngine(engine EngineInfo) VersionInfo {
	v.engine = some(copyEngine(engine))
	return v
}

// CLI returns the command-line build.
func (v VersionInfo) CLI() CLIBuild { return v.cli }

// Model returns the model library build.
func (v VersionInfo) Model() ModelBuild {
	model := v.model
	model.APIVersions = slices.Clone(model.APIVersions)
	return model
}

// Engine returns the linked engine's information, if any.
func (v VersionInfo) Engine() (EngineInfo, bool) {
	engine, ok := v.engine.get()
	return copyEngine(engine), ok
}

func copyEngine(engine EngineInfo) EngineInfo {
	engine.Capabilities = slices.Clone(engine.Capabilities)
	engine.StateRead = slices.Clone(engine.StateRead)
	engine.StoreRead = slices.Clone(engine.StoreRead)
	return engine
}

// EncodeVersionInfo returns the canonical VersionInfo document of v. Set-valued lists print sorted.
// It refuses, with an *Error, a VersionInfo that ParseVersionInfo would refuse (an empty version, a
// malformed capability, an unreadable API version), so it never sends a document the receiver
// rejects; the zero VersionInfo is such a value.
func EncodeVersionInfo(v VersionInfo) ([]byte, error) {
	apiVersions := slices.Clone(v.model.APIVersions)
	slices.SortFunc(apiVersions, compareText)
	document := canonical.Object{
		"apiVersion": canonical.String(APIVersion),
		"kind":       canonical.String(kindVersionInfo),
		"cli": canonical.Object{
			"version": canonical.String(v.cli.Version), "go": canonical.String(v.cli.Go), "platform": canonical.String(v.cli.Platform),
		},
		"model": canonical.Object{"version": canonical.String(v.model.Version), "apiVersions": stringArray(apiVersions)},
	}
	if engine, ok := v.engine.get(); ok {
		capabilities := make([]string, 0, len(engine.Capabilities))
		for _, capability := range engine.Capabilities {
			capabilities = append(capabilities, capability.value)
		}
		slices.SortFunc(capabilities, compareText)
		document["engine"] = canonical.Object{
			"version":       canonical.String(engine.Version),
			"capabilities":  stringArray(capabilities),
			"stateVersions": encodeVersionSet(engine.StateRead, engine.StateWrite),
			"storeVersions": encodeVersionSet(engine.StoreRead, engine.StoreWrite),
		}
	}
	encoded := canonical.Encode(document)
	if _, diagnostics := ParseVersionInfo(encoded); len(diagnostics) > 0 {
		first := diagnostics[0]
		return nil, invalidValue("version information is not valid: %s at %q: %s", first.Code(), first.Location().Pointer(), first.Message())
	}
	return encoded, nil
}

func stringArray(values []string) canonical.Array {
	array := canonical.Array{}
	for _, value := range values {
		array = append(array, canonical.String(value))
	}
	return array
}

func encodeVersionSet(read []int, write int) canonical.Object {
	sorted := slices.Clone(read)
	slices.Sort(sorted)
	array := canonical.Array{}
	for _, version := range sorted {
		array = append(array, canonical.Number(strconv.Itoa(version)))
	}
	return canonical.Object{"read": array, "write": canonical.Number(strconv.Itoa(write))}
}

type wireVersionSet struct {
	Read  []int `json:"read"`
	Write int   `json:"write"`
}

type wireVersionInfo struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	CLI        struct {
		Version  string `json:"version"`
		Go       string `json:"go"`
		Platform string `json:"platform"`
	} `json:"cli"`
	Model struct {
		Version     string   `json:"version"`
		APIVersions []string `json:"apiVersions"`
	} `json:"model"`
	Engine *struct {
		Version       string         `json:"version"`
		Capabilities  []string       `json:"capabilities"`
		StateVersions wireVersionSet `json:"stateVersions"`
		StoreVersions wireVersionSet `json:"storeVersions"`
	} `json:"engine"`
}

// ParseVersionInfo reads a VersionInfo document, such as another build's version output. On any
// diagnostic the VersionInfo is the zero value. Diagnostics carry no file name.
func ParseVersionInfo(data []byte) (VersionInfo, Diagnostics) {
	reader, diagnostics := newReader(nil)
	if len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics
	}
	doc, diagnostics := decodeDocument("", data)
	if len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics.Sorted()
	}
	if diagnostics := compatibility(doc); len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics
	}
	if diagnostics := reader.structure(doc, kindVersionInfo); len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics.Sorted()
	}
	var wire wireVersionInfo
	if diagnostics := doc.decode(&wire); len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics
	}
	info := NewVersionInfo(
		CLIBuild{Version: wire.CLI.Version, Go: wire.CLI.Go, Platform: wire.CLI.Platform},
		ModelBuild{Version: wire.Model.Version, APIVersions: wire.Model.APIVersions},
	)
	if wire.Engine == nil {
		return info, nil
	}
	convert := converter{}
	engine := EngineInfo{
		Version:    wire.Engine.Version,
		StateRead:  wire.Engine.StateVersions.Read,
		StateWrite: wire.Engine.StateVersions.Write,
		StoreRead:  wire.Engine.StoreVersions.Read,
		StoreWrite: wire.Engine.StoreVersions.Write,
	}
	for _, capability := range wire.Engine.Capabilities {
		engine.Capabilities = append(engine.Capabilities, convert.capability(capability))
	}
	if diagnostics := convert.diagnostics(doc, "/engine"); len(diagnostics) > 0 {
		return VersionInfo{}, diagnostics
	}
	return info.WithEngine(engine), nil
}
