package model

import (
	"slices"
)

// optional holds a value that may be absent, so absence never hides behind a zero value.
type optional[T any] struct {
	value T
	ok    bool
}

func some[T any](value T) optional[T] { return optional[T]{value: value, ok: true} }

func (o optional[T]) get() (T, bool) { return o.value, o.ok }

// WorkloadType says how a workload runs.
type WorkloadType int

const (
	// WorkloadService runs until it is stopped.
	WorkloadService WorkloadType = iota + 1
	// WorkloadJob runs to completion when it is invoked or required as a prerequisite.
	WorkloadJob
)

func workloadTypes() []WorkloadType { return []WorkloadType{WorkloadService, WorkloadJob} }

// String returns "service" or "job".
func (t WorkloadType) String() string {
	switch t {
	case WorkloadService:
		return "service"
	case WorkloadJob:
		return "job"
	default:
		return "unknown"
	}
}

// Workload is a container workload declared by a project or contributed by a Module Definition.
// It is immutable; slice accessors return copies.
type Workload struct {
	key           ObjectKey
	workloadType  WorkloadType
	artifact      Artifact
	command       []string
	environment   []EnvVar
	listeners     []Listener
	exports       []Export
	dependencies  []Dependency
	volumes       []Volume
	prerequisites []ObjectKey
	description   optional[string]
}

// Key returns the workload's key.
func (w Workload) Key() ObjectKey { return w.key }

// Type returns whether the workload is a service or a job.
func (w Workload) Type() WorkloadType { return w.workloadType }

// Artifact returns what the workload runs: an ImageArtifact or a BuildArtifact.
func (w Workload) Artifact() Artifact { return w.artifact }

// Command returns the argument vector that replaces the image's command, in order; empty when the
// image's own command runs.
func (w Workload) Command() []string { return slices.Clone(w.command) }

// Environment returns the declared environment variables, ordered by name.
func (w Workload) Environment() []EnvVar { return slices.Clone(w.environment) }

// Listeners returns the declared listeners, ordered by key.
func (w Workload) Listeners() []Listener { return slices.Clone(w.listeners) }

// Exports returns the declared Exports, ordered by key.
func (w Workload) Exports() []Export {
	exports := slices.Clone(w.exports)
	for index := range exports {
		exports[index].values = slices.Clone(exports[index].values)
	}
	return exports
}

// Dependencies returns the declared Dependencies, ordered by key.
func (w Workload) Dependencies() []Dependency { return slices.Clone(w.dependencies) }

// Volumes returns the declared persistent volumes, ordered by key.
func (w Workload) Volumes() []Volume { return slices.Clone(w.volumes) }

// Prerequisites returns the keys of the jobs that must complete before this workload starts,
// ordered by key.
func (w Workload) Prerequisites() []ObjectKey { return sortedObjectKeys(w.prerequisites) }

func sortedObjectKeys(keys []ObjectKey) []ObjectKey {
	sorted := slices.Clone(keys)
	slices.SortFunc(sorted, func(a, b ObjectKey) int { return compareText(a.value, b.value) })
	return sorted
}

// Description returns the workload's description, if one is declared.
func (w Workload) Description() (string, bool) { return w.description.get() }

func (w Workload) export(key ExportKey) (Export, bool) {
	for _, export := range w.exports {
		if export.Key == key {
			return export, true
		}
	}
	return Export{}, false
}

func (w Workload) dependency(key DependencyKey) (Dependency, bool) {
	for _, dependency := range w.dependencies {
		if dependency.Key == key {
			return dependency, true
		}
	}
	return Dependency{}, false
}

// Artifact is what a workload runs. Exactly one kind is set; resolving it to a recorded image is
// engine state, not model.
//
//sumtype:decl
type Artifact interface{ artifact() }

// ImageArtifact runs an existing image.
type ImageArtifact struct{ reference string }

func (ImageArtifact) artifact() {}

// Reference returns the image reference as declared: a tag, a digest or both.
func (a ImageArtifact) Reference() string { return a.reference }

// BuildArtifact builds an image from a Dockerfile.
type BuildArtifact struct {
	context    string
	dockerfile optional[string]
	target     optional[string]
}

func (BuildArtifact) artifact() {}

// Context returns the build context directory, relative to the project directory.
func (a BuildArtifact) Context() string { return a.context }

// Dockerfile returns the Dockerfile path relative to the context, if one is declared.
func (a BuildArtifact) Dockerfile() (string, bool) { return a.dockerfile.get() }

// Target returns the build stage, if one is declared.
func (a BuildArtifact) Target() (string, bool) { return a.target.get() }

// EnvVar is one environment variable of a workload.
type EnvVar struct {
	Key   EnvKey
	Value EnvValue
}

// EnvValue is the value of an environment variable: a LiteralValue or an InputValue.
//
//sumtype:decl
type EnvValue interface{ envValue() }

// LiteralValue is text written in the declaration.
type LiteralValue struct{ text string }

func (LiteralValue) envValue() {}

// Text returns the literal text.
func (v LiteralValue) Text() string { return v.text }

// InputValue takes its value from a declared input, resolved per environment.
type InputValue struct{ input InputKey }

func (InputValue) envValue() {}

// Input returns the input's key.
func (v InputValue) Input() InputKey { return v.input }

// Listener is a port a workload listens on and the protocol it speaks there.
type Listener struct {
	Key      ListenerKey
	Port     int
	Protocol string
}

// Sensitivity classifies a value for redaction.
type Sensitivity int

const (
	SensitivityPublic Sensitivity = iota + 1
	SensitivitySecret
)

func sensitivities() []Sensitivity { return []Sensitivity{SensitivityPublic, SensitivitySecret} }

// String returns "public" or "secret".
func (s Sensitivity) String() string {
	switch s {
	case SensitivityPublic:
		return "public"
	case SensitivitySecret:
		return "secret"
	default:
		return "unknown"
	}
}

// Export is a capability a workload offers on one of its listeners. Besides its declared values it
// always offers "host" and "port", resolved per consumer context.
type Export struct {
	Key        ExportKey
	Capability Capability
	Listener   ListenerKey
	values     []ExportValue
}

// Values returns the declared values, ordered by name; "host" and "port" are implicit.
func (e Export) Values() []ExportValue { return slices.Clone(e.values) }

// ExportValue is one named value of an Export and its sensitivity.
type ExportValue struct {
	Name        ValueName
	Sensitivity Sensitivity
}

// Dependency is a capability a consumer needs; a binding satisfies it with a matching Export.
type Dependency struct {
	Key        DependencyKey
	Capability Capability
}

// Volume is persistent storage mounted at a container path. Its data outlives the workload.
type Volume struct {
	Key  VolumeKey
	Path string
}

// implicitValues are the values every Export offers without declaring them.
func implicitValues() []string { return []string{"host", "port"} }

func (e Export) offers(name ValueName) bool {
	if slices.Contains(implicitValues(), name.String()) {
		return true
	}
	return slices.ContainsFunc(e.values, func(value ExportValue) bool { return value.Name == name })
}
