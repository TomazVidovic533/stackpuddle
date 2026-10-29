package model

import (
	"slices"
)

// Project is a loaded, checked project model: what Load returns for a Project or Model root. It is
// immutable and safe for concurrent use; slice accessors return copies.
type Project struct {
	name             ProjectName
	description      optional[string]
	manifestPath     optional[string]
	lockPath         optional[string]
	manifest         optional[Manifest]
	lock             optional[Lock]
	localDefinitions []localDefinition
	definitions      []ModuleDefinition
	unresolved       []ModuleName
	inputs           []Input
	workloads        []Workload
	modules          []ModuleInstance
	clients          []Client
	applications     []Application
	bindings         []Binding
	environments     []Environment
	provenance       map[Ref]Provenance
	// The indexes hold the same values as the slices above, keyed for the per-binding lookups.
	workloadsByKey    map[ObjectKey]Workload
	modulesByKey      map[ObjectKey]ModuleInstance
	definitionsByName map[ModuleName]ModuleDefinition
}

// indexBy maps each value to its key. Keys are unique: a Project keeps one declaration of each.
func indexBy[K comparable, T any](values []T, keyOf func(T) K) map[K]T {
	index := make(map[K]T, len(values))
	for _, value := range values {
		index[keyOf(value)] = value
	}
	return index
}

// localDefinition is a project-local definition and its directory, relative to the project.
type localDefinition struct {
	name ModuleName
	path string
}

// Name returns the project's name.
func (p Project) Name() ProjectName { return p.name }

// Workloads returns the declared workloads, ordered by key.
func (p Project) Workloads() []Workload { return slices.Clone(p.workloads) }

// ModuleInstances returns the declared module instances, ordered by key.
func (p Project) ModuleInstances() []ModuleInstance { return slices.Clone(p.modules) }

// Clients returns the declared clients, ordered by key.
func (p Project) Clients() []Client { return slices.Clone(p.clients) }

// Applications returns the declared applications, ordered by key.
func (p Project) Applications() []Application { return slices.Clone(p.applications) }

// Bindings returns the declared bindings, ordered by binding reference.
func (p Project) Bindings() []Binding { return slices.Clone(p.bindings) }

// Inputs returns the declared inputs, ordered by key.
func (p Project) Inputs() []Input { return slices.Clone(p.inputs) }

// Environments returns the declared environments, ordered by name.
func (p Project) Environments() []Environment { return slices.Clone(p.environments) }

// Environment returns the environment with the given name, if it is declared.
func (p Project) Environment(name EnvironmentName) (Environment, bool) {
	for _, environment := range p.environments {
		if environment.name == name {
			return environment, true
		}
	}
	return Environment{}, false
}

// Definition returns the resolved Module Definition with the given name, local or packaged.
func (p Project) Definition(name ModuleName) (ModuleDefinition, bool) {
	definition, ok := p.definitionsByName[name]
	return definition, ok
}

// Unresolved returns the names of definitions the Manifest lists but the caller did not supply,
// sorted. Every check that needs such a definition was skipped, not passed; a caller that
// provisions must refuse a project while this is non-empty.
func (p Project) Unresolved() []ModuleName { return slices.Clone(p.unresolved) }

// Manifest returns the project's Manifest, if it names one.
func (p Project) Manifest() (Manifest, bool) { return p.manifest.get() }

// Lock returns the project's Lock, if it names one.
func (p Project) Lock() (Lock, bool) { return p.lock.get() }

// Provenance returns where the declaration ref names came from: a workload, module, client, app,
// env or binding reference.
func (p Project) Provenance(ref Ref) (Provenance, bool) {
	provenance, ok := p.provenance[ref]
	return provenance, ok
}

// RequiredCapabilities returns, sorted, the capabilities an engine must advertise to run the named
// environment: its runtime, its secret provider and every resolved definition's requirements.
// Definitions in Unresolved contribute nothing. ok is false when the environment is not declared.
func (p Project) RequiredCapabilities(name EnvironmentName) ([]Capability, bool) {
	environment, ok := p.Environment(name)
	if !ok {
		return nil, false
	}
	required := []Capability{runtimeCapability(environment.runtime), secretsCapability(environment.secretProvider)}
	for _, instance := range p.modules {
		if definition, resolved := p.Definition(instance.definition); resolved {
			required = append(required, definition.requires...)
		}
	}
	slices.SortFunc(required, func(a, b Capability) int { return compareText(a.value, b.value) })
	return slices.Compact(required), true
}

func runtimeCapability(runtime Runtime) Capability {
	switch runtime.(type) {
	case ComposeRuntime:
		return Capability{value: "runtime." + composeRuntimeType + "/1"}
	default:
		return Capability{value: "runtime.unknown/1"}
	}
}

func secretsCapability(provider ProviderID) Capability {
	return Capability{value: "secrets." + provider.value + "/1"}
}

func (p Project) workload(key ObjectKey) (Workload, bool) {
	workload, ok := p.workloadsByKey[key]
	return workload, ok
}

func (p Project) module(key ObjectKey) (ModuleInstance, bool) {
	instance, ok := p.modulesByKey[key]
	return instance, ok
}

// Client is a process on the host that StackPuddle does not run, such as an IDE or a CLI tool,
// which consumes bindings from the host context.
type Client struct {
	key          ObjectKey
	dependencies []Dependency
	description  optional[string]
}

// Key returns the client's key.
func (c Client) Key() ObjectKey { return c.key }

// Dependencies returns the client's Dependencies, ordered by key.
func (c Client) Dependencies() []Dependency { return slices.Clone(c.dependencies) }

// Application is a named group of workloads selected together. Groups may overlap; a workload in
// two groups is still one workload.
type Application struct {
	key         ObjectKey
	workloads   []ObjectKey
	isDefault   bool
	description optional[string]
}

// Key returns the application's key.
func (a Application) Key() ObjectKey { return a.key }

// Workloads returns the member workload keys, sorted.
func (a Application) Workloads() []ObjectKey { return sortedObjectKeys(a.workloads) }

// Default reports whether the application is selected when none is named.
func (a Application) Default() bool { return a.isDefault }
