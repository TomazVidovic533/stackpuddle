package model

import (
	"encoding/json"
	"slices"
)

// Input is a value a project declares once and each environment supplies. A secret input never
// carries its value in the model; its sources name where the value comes from.
type Input struct {
	key         InputKey
	sensitivity Sensitivity
	value       optional[string]
}

// Key returns the input's key.
func (i Input) Key() InputKey { return i.key }

// Sensitivity returns whether the input is public or secret.
func (i Input) Sensitivity() Sensitivity { return i.sensitivity }

// Value returns the project-level value of a public input, if one is declared.
func (i Input) Value() (string, bool) { return i.value.get() }

// Environment is one declared target: the runtime and Docker context it selects, its secret
// provider, host exposures, input sources and overrides.
type Environment struct {
	name              EnvironmentName
	runtime           Runtime
	secretProvider    ProviderID
	exposures         []Exposure
	inputSources      []InputSources
	workloadOverrides []WorkloadOverride
	moduleOverrides   []ModuleOverride
	// moduleOverrideAt is where each module override's options sit in the environment's document.
	moduleOverrideAt map[ObjectKey]origin
	description      optional[string]
}

// Name returns the environment's name.
func (e Environment) Name() EnvironmentName { return e.name }

// Runtime returns the runtime the environment selects.
func (e Environment) Runtime() Runtime { return e.runtime }

// SecretProvider returns the secret provider the environment names. The engine checks it against
// what it advertises.
func (e Environment) SecretProvider() ProviderID { return e.secretProvider }

// Exposures returns the Exports published on the host loopback interface, ordered by object and
// Export.
func (e Environment) Exposures() []Exposure {
	exposures := slices.Clone(e.exposures)
	slices.SortStableFunc(exposures, func(a, b Exposure) int { return compareExportRefs(a.Source, b.Source) })
	return exposures
}

// InputSources returns where the environment takes each input's value, ordered by input key.
func (e Environment) InputSources() []InputSources { return slices.Clone(e.inputSources) }

// WorkloadOverrides returns the environment's workload environment-variable overrides, ordered by
// workload key.
func (e Environment) WorkloadOverrides() []WorkloadOverride {
	overrides := slices.Clone(e.workloadOverrides)
	for index := range overrides {
		overrides[index].Environment = slices.Clone(overrides[index].Environment)
	}
	return overrides
}

// ModuleOverrides returns the environment's module option overrides, ordered by instance key.
func (e Environment) ModuleOverrides() []ModuleOverride {
	overrides := slices.Clone(e.moduleOverrides)
	for index := range overrides {
		overrides[index].Options = slices.Clone(overrides[index].Options)
	}
	return overrides
}

func (e Environment) exposes(source ExportRef) bool {
	return slices.ContainsFunc(e.exposures, func(exposure Exposure) bool { return exposure.Source == source })
}

// Exposure publishes one Export on the host loopback interface.
type Exposure struct {
	Source   ExportRef
	hostPort optional[int]
}

// HostPort returns the fixed host port, if declared; otherwise the port is assigned at run time.
func (e Exposure) HostPort() (int, bool) { return e.hostPort.get() }

// InputSources says where one environment takes an input's value. Precedence among the sources is
// the configuration layer's to apply, not the model's.
type InputSources struct {
	Key     InputKey
	value   optional[string]
	local   optional[LocalSource]
	process optional[EnvKey]
}

// Value returns the literal value the environment sets, if any. Secret inputs never have one.
func (s InputSources) Value() (string, bool) { return s.value.get() }

// Local returns the ignored local file and key that supply the value, if declared.
func (s InputSources) Local() (LocalSource, bool) { return s.local.get() }

// Process returns the process environment variable explicitly mapped to the input, if declared.
func (s InputSources) Process() (EnvKey, bool) { return s.process.get() }

// LocalSource names a key in a local, ignored file, relative to the project directory.
type LocalSource struct {
	File string
	Key  EnvKey
}

// WorkloadOverride replaces or adds environment variables of one workload in one environment.
type WorkloadOverride struct {
	Key         ObjectKey
	Environment []EnvVar
}

// ModuleOverride changes one instance's options in one environment: scalars and lists replace,
// objects merge by key. Options holds canonical JSON bytes.
type ModuleOverride struct {
	Key     ObjectKey
	Options json.RawMessage
}

// Runtime is the runtime engine an environment selects.
//
//sumtype:decl
type Runtime interface{ runtime() }

// ComposeRuntime runs the environment with Docker Compose on one named Docker context. The context
// is explicit, so an ambient context never silently replaces it.
type ComposeRuntime struct{ context string }

// composeRuntimeType is how a document names ComposeRuntime, in its runtime type and capability.
const composeRuntimeType = "compose"

func (ComposeRuntime) runtime() {}

// Context returns the Docker context name.
func (r ComposeRuntime) Context() string { return r.context }
