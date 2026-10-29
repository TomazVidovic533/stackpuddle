package model

import (
	"encoding/json"
	"slices"
	"strconv"
)

// The wire structs mirror schemas/v1 member for member. They are decoded only from schema-valid
// trees with unknown members refused, so a member the schema adds and these lack fails every
// conformance case that uses it.

type wireMetadata struct {
	Description *string `json:"description"`
}

type wireProjectMetadata struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type wireListener struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type wireExportValue struct {
	Sensitivity string `json:"sensitivity"`
}

type wireExport struct {
	Capability string                     `json:"capability"`
	Listener   string                     `json:"listener"`
	Values     map[string]wireExportValue `json:"values"`
}

type wireDependency struct {
	Capability string `json:"capability"`
}

type wireVolume struct {
	Path string `json:"path"`
}

type wireBuild struct {
	Context    string  `json:"context"`
	Dockerfile *string `json:"dockerfile"`
	Target     *string `json:"target"`
}

type wirePrerequisite struct {
	Job string `json:"job"`
}

type wireWorkloadSpec struct {
	Type          string                     `json:"type"`
	Image         *string                    `json:"image"`
	Build         *wireBuild                 `json:"build"`
	Command       []string                   `json:"command"`
	Environment   map[string]json.RawMessage `json:"environment"`
	Listeners     map[string]wireListener    `json:"listeners"`
	Exports       map[string]wireExport      `json:"exports"`
	Dependencies  map[string]wireDependency  `json:"dependencies"`
	Volumes       map[string]wireVolume      `json:"volumes"`
	Prerequisites []wirePrerequisite         `json:"prerequisites"`
}

type wireWorkloadDeclaration struct {
	wireWorkloadSpec
	Metadata *wireMetadata `json:"metadata"`
}

type wireModuleSpec struct {
	Definition string          `json:"definition"`
	Options    json.RawMessage `json:"options"`
}

type wireModuleDeclaration struct {
	wireModuleSpec
	Metadata *wireMetadata `json:"metadata"`
}

type wireClientSpec struct {
	Dependencies map[string]wireDependency `json:"dependencies"`
}

type wireClientDeclaration struct {
	wireClientSpec
	Metadata *wireMetadata `json:"metadata"`
}

type wireApplication struct {
	Workloads []string      `json:"workloads"`
	Default   *bool         `json:"default"`
	Metadata  *wireMetadata `json:"metadata"`
}

type wireExportRef struct {
	Object string `json:"object"`
	Export string `json:"export"`
}

type wireFileDelivery struct {
	Path  string  `json:"path"`
	Mode  string  `json:"mode"`
	Owner *string `json:"owner"`
}

type wireDelivery struct {
	Environment map[string]string           `json:"environment"`
	Files       map[string]wireFileDelivery `json:"files"`
}

type wireBinding struct {
	Consumer   string          `json:"consumer"`
	Dependency string          `json:"dependency"`
	Source     wireExportRef   `json:"source"`
	Parameters json.RawMessage `json:"parameters"`
	Delivery   *wireDelivery   `json:"delivery"`
}

type wireInput struct {
	Sensitivity string  `json:"sensitivity"`
	Value       *string `json:"value"`
}

type wireLocalSource struct {
	File string `json:"file"`
	Key  string `json:"key"`
}

type wireInputSources struct {
	Value   *string          `json:"value"`
	Local   *wireLocalSource `json:"local"`
	Process *string          `json:"process"`
}

type wireRuntime struct {
	Type    string `json:"type"`
	Context string `json:"context"`
}

type wireSecrets struct {
	Provider string `json:"provider"`
}

type wireExposure struct {
	Source wireExportRef `json:"source"`
	Port   *int          `json:"port"`
}

type wireWorkloadOverride struct {
	Environment map[string]json.RawMessage `json:"environment"`
}

type wireModuleOverride struct {
	Options json.RawMessage `json:"options"`
}

type wireOverrides struct {
	Workloads map[string]wireWorkloadOverride `json:"workloads"`
	Modules   map[string]wireModuleOverride   `json:"modules"`
}

type wireEnvironment struct {
	Runtime   wireRuntime                 `json:"runtime"`
	Secrets   wireSecrets                 `json:"secrets"`
	Exposures []wireExposure              `json:"exposures"`
	Inputs    map[string]wireInputSources `json:"inputs"`
	Overrides *wireOverrides              `json:"overrides"`
	Metadata  *wireMetadata               `json:"metadata"`
}

type wireDefinitionEntry struct {
	Path string `json:"path"`
}

type wireSections struct {
	Definitions  map[string]wireDefinitionEntry     `json:"definitions"`
	Inputs       map[string]wireInput               `json:"inputs"`
	Workloads    map[string]wireWorkloadDeclaration `json:"workloads"`
	Modules      map[string]wireModuleDeclaration   `json:"modules"`
	Clients      map[string]wireClientDeclaration   `json:"clients"`
	Applications map[string]wireApplication         `json:"applications"`
	Bindings     []wireBinding                      `json:"bindings"`
	Environments map[string]wireEnvironment         `json:"environments"`
}

type wireProjectSpec struct {
	wireSections
	Imports  []string `json:"imports"`
	Manifest *string  `json:"manifest"`
	Lock     *string  `json:"lock"`
}

type wireProject struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   wireProjectMetadata `json:"metadata"`
	Spec       wireProjectSpec     `json:"spec"`
}

type wireFragment struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Spec       wireSections `json:"spec"`
}

type wireObject struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Key        string          `json:"key"`
	Metadata   *wireMetadata   `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type wireModelSpec struct {
	Manifest     *string                        `json:"manifest"`
	Lock         *string                        `json:"lock"`
	Definitions  map[string]wireDefinitionEntry `json:"definitions"`
	Inputs       map[string]wireInput           `json:"inputs"`
	Objects      []wireObject                   `json:"objects"`
	Applications map[string]wireApplication     `json:"applications"`
	Bindings     []wireBinding                  `json:"bindings"`
	Environments map[string]wireEnvironment     `json:"environments"`
}

type wireOrigin struct {
	Origin string  `json:"origin"`
	File   *string `json:"file"`
	Line   *int    `json:"line"`
	Column *int    `json:"column"`
}

type wirePatch struct {
	Origin string   `json:"origin"`
	Fields []string `json:"fields"`
	File   *string  `json:"file"`
	Line   *int     `json:"line"`
	Column *int     `json:"column"`
}

type wireProvenanceEntry struct {
	Declared wireOrigin  `json:"declared"`
	Patched  []wirePatch `json:"patched"`
}

type wireModel struct {
	APIVersion string                         `json:"apiVersion"`
	Kind       string                         `json:"kind"`
	Metadata   wireProjectMetadata            `json:"metadata"`
	Spec       wireModelSpec                  `json:"spec"`
	Provenance map[string]wireProvenanceEntry `json:"provenance"`
}

type wireRequires struct {
	Capabilities []string `json:"capabilities"`
}

type wireConfiguration struct {
	Schema   string  `json:"schema"`
	Defaults *string `json:"defaults"`
}

type wireComposeRuntime struct {
	File           string  `json:"file"`
	HostAccessFile *string `json:"hostAccessFile"`
}

type wireRuntimes struct {
	Compose *wireComposeRuntime `json:"compose"`
}

type wireProvisioning struct {
	Contract string `json:"contract"`
}

type wireModule struct {
	APIVersion    string                             `json:"apiVersion"`
	Kind          string                             `json:"kind"`
	Name          string                             `json:"name"`
	Version       string                             `json:"version"`
	DataFormat    *string                            `json:"dataFormat"`
	Requires      *wireRequires                      `json:"requires"`
	Configuration *wireConfiguration                 `json:"configuration"`
	Runtimes      *wireRuntimes                      `json:"runtimes"`
	Provisioning  *wireProvisioning                  `json:"provisioning"`
	Workloads     map[string]wireWorkloadDeclaration `json:"workloads"`
}

type wireBundleModule struct {
	Version string `json:"version"`
}

type wireBundle struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Version    string                      `json:"version"`
	Modules    map[string]wireBundleModule `json:"modules"`
}

type wireManifest struct {
	APIVersion    string   `json:"apiVersion"`
	Kind          string   `json:"kind"`
	BundleVersion string   `json:"bundleVersion"`
	Modules       []string `json:"modules"`
}

type wireArtifact struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	Digest  string `json:"digest"`
}

type wireLockedModule struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	Format  string `json:"format"`
	Digest  string `json:"digest"`
}

type wireLock struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Bundle     wireArtifact                `json:"bundle"`
	Modules    map[string]wireLockedModule `json:"modules"`
}

// converter turns schema-valid wire values into domain values. The schema already enforces every
// rule the Parse functions check, so a failure here means the two disagree; the first one is kept
// and reported instead of being dropped.
type converter struct {
	err error
}

func (c *converter) keep(err error) {
	if c.err == nil && err != nil {
		c.err = err
	}
}

func (c *converter) diagnostics(doc *document, pointer string) Diagnostics {
	if c.err == nil {
		return nil
	}
	return Diagnostics{newDiagnostic(StructureInvalidValue, doc.at(pointer), "a value passed the schema but not the model's rules: "+c.err.Error())}
}

func (c *converter) projectName(s string) ProjectName {
	value, err := ParseProjectName(s)
	c.keep(err)
	return value
}

func (c *converter) environmentName(s string) EnvironmentName {
	value, err := ParseEnvironmentName(s)
	c.keep(err)
	return value
}

func (c *converter) objectKey(s string) ObjectKey {
	value, err := ParseObjectKey(s)
	c.keep(err)
	return value
}

func (c *converter) objectRef(s string) ObjectRef {
	value, err := ParseObjectRef(s)
	c.keep(err)
	return value
}

func (c *converter) moduleName(s string) ModuleName {
	value, err := ParseModuleName(s)
	c.keep(err)
	return value
}

func (c *converter) dependencyKey(s string) DependencyKey {
	value, err := ParseDependencyKey(s)
	c.keep(err)
	return value
}

func (c *converter) exportKey(s string) ExportKey {
	value, err := ParseExportKey(s)
	c.keep(err)
	return value
}

func (c *converter) listenerKey(s string) ListenerKey {
	value, err := ParseListenerKey(s)
	c.keep(err)
	return value
}

func (c *converter) volumeKey(s string) VolumeKey {
	value, err := ParseVolumeKey(s)
	c.keep(err)
	return value
}

func (c *converter) inputKey(s string) InputKey {
	value, err := ParseInputKey(s)
	c.keep(err)
	return value
}

func (c *converter) valueName(s string) ValueName {
	value, err := ParseValueName(s)
	c.keep(err)
	return value
}

func (c *converter) envKey(s string) EnvKey {
	value, err := ParseEnvKey(s)
	c.keep(err)
	return value
}

func (c *converter) capability(s string) Capability {
	value, err := ParseCapability(s)
	c.keep(err)
	return value
}

func (c *converter) dataFormat(s string) DataFormat {
	value, err := ParseDataFormat(s)
	c.keep(err)
	return value
}

func (c *converter) providerID(s string) ProviderID {
	value, err := ParseProviderID(s)
	c.keep(err)
	return value
}

func (c *converter) version(s string) Version {
	value, err := ParseVersion(s)
	c.keep(err)
	return value
}

func (c *converter) canonicalJSON(raw json.RawMessage) []byte {
	data, err := canonicalBytes(raw)
	c.keep(err)
	return data
}

func (c *converter) sensitivity(s string) Sensitivity {
	sensitivity, known := parseClosed(sensitivities(), Sensitivity.String, s)
	if !known {
		c.keep(invalidValue("unknown sensitivity"))
	}
	return sensitivity
}

func (c *converter) fileMode(s string) FileMode {
	mode, err := strconv.ParseUint(s, 8, 32)
	c.keep(err)
	return FileMode(mode)
}

func optionalOf[T any](pointer *T) optional[T] {
	if pointer == nil {
		return optional[T]{}
	}
	return some(*pointer)
}

func descriptionOf(metadata *wireMetadata) optional[string] {
	if metadata == nil {
		return optional[string]{}
	}
	return optionalOf(metadata.Description)
}

func (c *converter) workload(key string, spec wireWorkloadSpec, metadata *wireMetadata) Workload {
	workload := Workload{
		key:         c.objectKey(key),
		command:     slices.Clone(spec.Command),
		environment: c.envVars(spec.Environment),
		description: descriptionOf(metadata),
	}
	workloadType, known := parseClosed(workloadTypes(), WorkloadType.String, spec.Type)
	if !known {
		c.keep(invalidValue("unknown workload type"))
	}
	workload.workloadType = workloadType
	switch {
	case spec.Image != nil:
		workload.artifact = ImageArtifact{reference: *spec.Image}
	case spec.Build != nil:
		workload.artifact = BuildArtifact{context: spec.Build.Context, dockerfile: optionalOf(spec.Build.Dockerfile), target: optionalOf(spec.Build.Target)}
	default:
		c.keep(invalidValue("a workload needs an image or a build"))
	}
	for _, name := range sortedKeys(spec.Listeners) {
		listener := spec.Listeners[name]
		workload.listeners = append(workload.listeners, Listener{Key: c.listenerKey(name), Port: listener.Port, Protocol: listener.Protocol})
	}
	for _, name := range sortedKeys(spec.Exports) {
		export := spec.Exports[name]
		converted := Export{Key: c.exportKey(name), Capability: c.capability(export.Capability), Listener: c.listenerKey(export.Listener)}
		for _, valueName := range sortedKeys(export.Values) {
			converted.values = append(converted.values, ExportValue{Name: c.valueName(valueName), Sensitivity: c.sensitivity(export.Values[valueName].Sensitivity)})
		}
		workload.exports = append(workload.exports, converted)
	}
	workload.dependencies = c.dependencies(spec.Dependencies)
	for _, name := range sortedKeys(spec.Volumes) {
		workload.volumes = append(workload.volumes, Volume{Key: c.volumeKey(name), Path: spec.Volumes[name].Path})
	}
	for _, prerequisite := range spec.Prerequisites {
		workload.prerequisites = append(workload.prerequisites, c.objectKey(prerequisite.Job))
	}
	return workload
}

func (c *converter) dependencies(wire map[string]wireDependency) []Dependency {
	var dependencies []Dependency
	for _, name := range sortedKeys(wire) {
		dependencies = append(dependencies, Dependency{Key: c.dependencyKey(name), Capability: c.capability(wire[name].Capability)})
	}
	return dependencies
}

func (c *converter) envVars(wire map[string]json.RawMessage) []EnvVar {
	var variables []EnvVar
	for _, name := range sortedKeys(wire) {
		variables = append(variables, EnvVar{Key: c.envKey(name), Value: c.envValue(wire[name])})
	}
	return variables
}

func (c *converter) envValue(raw json.RawMessage) EnvValue {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return LiteralValue{text: text}
	}
	var reference struct {
		Input string `json:"input"`
	}
	c.keep(decodeStrict(raw, &reference))
	return InputValue{input: c.inputKey(reference.Input)}
}

func (c *converter) moduleInstance(key string, spec wireModuleSpec, metadata *wireMetadata, optionsAt origin) ModuleInstance {
	return ModuleInstance{
		key:         c.objectKey(key),
		definition:  c.moduleName(spec.Definition),
		options:     c.canonicalJSON(spec.Options),
		description: descriptionOf(metadata),
		optionsAt:   optionsAt,
	}
}

func (c *converter) client(key string, spec wireClientSpec, metadata *wireMetadata) Client {
	return Client{key: c.objectKey(key), dependencies: c.dependencies(spec.Dependencies), description: descriptionOf(metadata)}
}

func (c *converter) application(key string, wire wireApplication) Application {
	application := Application{key: c.objectKey(key), description: descriptionOf(wire.Metadata)}
	if wire.Default != nil {
		application.isDefault = *wire.Default
	}
	for _, member := range wire.Workloads {
		application.workloads = append(application.workloads, c.objectKey(member))
	}
	return application
}

func (c *converter) exportRef(wire wireExportRef) ExportRef {
	return ExportRef{Object: c.objectRef(wire.Object), Export: c.exportKey(wire.Export)}
}

func (c *converter) binding(wire wireBinding, at origin) Binding {
	binding := Binding{
		at:     at,
		ref:    BindingRef{Consumer: c.objectRef(wire.Consumer), Dependency: c.dependencyKey(wire.Dependency)},
		source: c.exportRef(wire.Source),
	}
	if parameters := c.canonicalJSON(wire.Parameters); len(wire.Parameters) > 0 && !isEmptyObject(parameters) {
		binding.parameters = some(parameters)
	}
	if wire.Delivery != nil && len(wire.Delivery.Environment)+len(wire.Delivery.Files) > 0 {
		var delivery Delivery
		for _, name := range sortedKeys(wire.Delivery.Environment) {
			delivery.environment = append(delivery.environment, EnvDelivery{Key: c.envKey(name), Value: c.valueName(wire.Delivery.Environment[name])})
		}
		for _, name := range sortedKeys(wire.Delivery.Files) {
			file := wire.Delivery.Files[name]
			delivery.files = append(delivery.files, FileDelivery{Value: c.valueName(name), Path: file.Path, Mode: c.fileMode(file.Mode), owner: optionalOf(file.Owner)})
		}
		binding.delivery = some(delivery)
	}
	return binding
}

func isEmptyObject(canonicalJSON []byte) bool { return string(canonicalJSON) == "{}\n" }

func (c *converter) input(key string, wire wireInput) Input {
	return Input{key: c.inputKey(key), sensitivity: c.sensitivity(wire.Sensitivity), value: optionalOf(wire.Value)}
}

func (c *converter) environment(name string, wire wireEnvironment) Environment {
	if wire.Runtime.Type != composeRuntimeType {
		c.keep(invalidValue("unknown runtime type"))
	}
	environment := Environment{
		name:           c.environmentName(name),
		runtime:        ComposeRuntime{context: wire.Runtime.Context},
		secretProvider: c.providerID(wire.Secrets.Provider),
		description:    descriptionOf(wire.Metadata),
	}
	for _, exposure := range wire.Exposures {
		environment.exposures = append(environment.exposures, Exposure{Source: c.exportRef(exposure.Source), hostPort: optionalOf(exposure.Port)})
	}
	for _, key := range sortedKeys(wire.Inputs) {
		sources := wire.Inputs[key]
		converted := InputSources{Key: c.inputKey(key), value: optionalOf(sources.Value)}
		if sources.Local != nil {
			converted.local = some(LocalSource{File: sources.Local.File, Key: c.envKey(sources.Local.Key)})
		}
		if sources.Process != nil {
			converted.process = some(c.envKey(*sources.Process))
		}
		environment.inputSources = append(environment.inputSources, converted)
	}
	if wire.Overrides != nil {
		for _, key := range sortedKeys(wire.Overrides.Workloads) {
			environment.workloadOverrides = append(environment.workloadOverrides, WorkloadOverride{
				Key: c.objectKey(key), Environment: c.envVars(wire.Overrides.Workloads[key].Environment),
			})
		}
		for _, key := range sortedKeys(wire.Overrides.Modules) {
			environment.moduleOverrides = append(environment.moduleOverrides, ModuleOverride{
				Key: c.objectKey(key), Options: c.canonicalJSON(wire.Overrides.Modules[key].Options),
			})
		}
	}
	return environment
}

func compareExportRefs(a, b ExportRef) int {
	if c := compareText(a.Object.String(), b.Object.String()); c != 0 {
		return c
	}
	return compareText(a.Export.value, b.Export.value)
}
