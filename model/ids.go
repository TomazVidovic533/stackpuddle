package model

import (
	"regexp"
)

// Each rule here mirrors one "$defs" entry of schemas/v1/definitions.schema.json; the identifier
// unit tests check both sides with the same table.
var (
	kebabName      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	capabilityName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*(?:\.[a-z][a-z0-9]*(?:-[a-z0-9]+)*)+/[1-9][0-9]*$`)
	dataFormatName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*/[0-9]+(?:\.[0-9]+)*$`)
	envKeyName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Name limits are tight on purpose: loosening one later is additive, tightening one is breaking.
const (
	scopeNameMax  = 32
	objectKeyMax  = 30
	moduleNameMax = 63
	capabilityMax = 128
	dataFormatMax = 64
	providerIDMax = 32
	envKeyMax     = 128
)

func checkName(label, s string, pattern *regexp.Regexp, maxLength int) error {
	if s == "" || len(s) > maxLength || !pattern.MatchString(s) {
		return invalidValue("%s %q is not valid: it needs 1 to %d characters matching %s", label, s, maxLength, pattern)
	}
	return nil
}

// ProjectName names a project: 1-32 lowercase letters and digits in hyphen-separated words, starting
// with a letter. It never contains ".", because state folders are named "<project>.<environment>".
type ProjectName struct{ value string }

// ParseProjectName returns s as a ProjectName, or an *Error with code structure.invalid_value.
func ParseProjectName(s string) (ProjectName, error) {
	if err := checkName("project name", s, kebabName, scopeNameMax); err != nil {
		return ProjectName{}, err
	}
	return ProjectName{value: s}, nil
}

// String returns the name.
func (n ProjectName) String() string { return n.value }

// EnvironmentName names an environment of a project, with the rules of ProjectName.
type EnvironmentName struct{ value string }

// ParseEnvironmentName returns s as an EnvironmentName, or an *Error with code structure.invalid_value.
func ParseEnvironmentName(s string) (EnvironmentName, error) {
	if err := checkName("environment name", s, kebabName, scopeNameMax); err != nil {
		return EnvironmentName{}, err
	}
	return EnvironmentName{value: s}, nil
}

// String returns the name.
func (n EnvironmentName) String() string { return n.value }

// ObjectKey is the key of a declared object (workload, module instance, client) or application:
// 1-30 lowercase letters and digits in hyphen-separated words, starting with a letter.
type ObjectKey struct{ value string }

// ParseObjectKey returns s as an ObjectKey, or an *Error with code structure.invalid_value.
func ParseObjectKey(s string) (ObjectKey, error) {
	if err := checkName("object key", s, kebabName, objectKeyMax); err != nil {
		return ObjectKey{}, err
	}
	return ObjectKey{value: s}, nil
}

// String returns the key.
func (k ObjectKey) String() string { return k.value }

// ModuleName names a Module Definition: 1-63 lowercase letters and digits in hyphen-separated words.
type ModuleName struct{ value string }

// ParseModuleName returns s as a ModuleName, or an *Error with code structure.invalid_value.
func ParseModuleName(s string) (ModuleName, error) {
	if err := checkName("module name", s, kebabName, moduleNameMax); err != nil {
		return ModuleName{}, err
	}
	return ModuleName{value: s}, nil
}

// String returns the name.
func (n ModuleName) String() string { return n.value }

// DependencyKey is the key of a Dependency, with the rules of ObjectKey.
type DependencyKey struct{ value string }

// ParseDependencyKey returns s as a DependencyKey, or an *Error with code structure.invalid_value.
func ParseDependencyKey(s string) (DependencyKey, error) {
	if err := checkName("dependency key", s, kebabName, objectKeyMax); err != nil {
		return DependencyKey{}, err
	}
	return DependencyKey{value: s}, nil
}

// String returns the key.
func (k DependencyKey) String() string { return k.value }

// ExportKey is the key of an Export, with the rules of ObjectKey.
type ExportKey struct{ value string }

// ParseExportKey returns s as an ExportKey, or an *Error with code structure.invalid_value.
func ParseExportKey(s string) (ExportKey, error) {
	if err := checkName("export key", s, kebabName, objectKeyMax); err != nil {
		return ExportKey{}, err
	}
	return ExportKey{value: s}, nil
}

// String returns the key.
func (k ExportKey) String() string { return k.value }

// ListenerKey is the key of a listener, with the rules of ObjectKey.
type ListenerKey struct{ value string }

// ParseListenerKey returns s as a ListenerKey, or an *Error with code structure.invalid_value.
func ParseListenerKey(s string) (ListenerKey, error) {
	if err := checkName("listener key", s, kebabName, objectKeyMax); err != nil {
		return ListenerKey{}, err
	}
	return ListenerKey{value: s}, nil
}

// String returns the key.
func (k ListenerKey) String() string { return k.value }

// VolumeKey is the key of a volume, with the rules of ObjectKey.
type VolumeKey struct{ value string }

// ParseVolumeKey returns s as a VolumeKey, or an *Error with code structure.invalid_value.
func ParseVolumeKey(s string) (VolumeKey, error) {
	if err := checkName("volume key", s, kebabName, objectKeyMax); err != nil {
		return VolumeKey{}, err
	}
	return VolumeKey{value: s}, nil
}

// String returns the key.
func (k VolumeKey) String() string { return k.value }

// InputKey is the key of a declared input, with the rules of ObjectKey.
type InputKey struct{ value string }

// ParseInputKey returns s as an InputKey, or an *Error with code structure.invalid_value.
func ParseInputKey(s string) (InputKey, error) {
	if err := checkName("input key", s, kebabName, objectKeyMax); err != nil {
		return InputKey{}, err
	}
	return InputKey{value: s}, nil
}

// String returns the key.
func (k InputKey) String() string { return k.value }

// ValueName names a value an Export offers: "host", "port" or a declared value, with the rules of
// ObjectKey.
type ValueName struct{ value string }

// ParseValueName returns s as a ValueName, or an *Error with code structure.invalid_value.
func ParseValueName(s string) (ValueName, error) {
	if err := checkName("value name", s, kebabName, objectKeyMax); err != nil {
		return ValueName{}, err
	}
	return ValueName{value: s}, nil
}

// String returns the name.
func (n ValueName) String() string { return n.value }

// EnvKey is a process environment variable name: a letter or "_" then letters, digits and "_",
// at most 128 characters.
type EnvKey struct{ value string }

// ParseEnvKey returns s as an EnvKey, or an *Error with code structure.invalid_value.
func ParseEnvKey(s string) (EnvKey, error) {
	if err := checkName("environment variable name", s, envKeyName, envKeyMax); err != nil {
		return EnvKey{}, err
	}
	return EnvKey{value: s}, nil
}

// String returns the name.
func (k EnvKey) String() string { return k.value }

// Capability names a versioned capability, "<dotted name>/<major>", such as "module.postgres/1".
type Capability struct{ value string }

// ParseCapability returns s as a Capability, or an *Error with code structure.invalid_value.
func ParseCapability(s string) (Capability, error) {
	if err := checkName("capability", s, capabilityName, capabilityMax); err != nil {
		return Capability{}, err
	}
	return Capability{value: s}, nil
}

// String returns the capability.
func (c Capability) String() string { return c.value }

// DataFormat names the format of data a module keeps, "<name>/<version>", such as "postgresql/18".
type DataFormat struct{ value string }

// ParseDataFormat returns s as a DataFormat, or an *Error with code structure.invalid_value.
func ParseDataFormat(s string) (DataFormat, error) {
	if err := checkName("data format", s, dataFormatName, dataFormatMax); err != nil {
		return DataFormat{}, err
	}
	return DataFormat{value: s}, nil
}

// String returns the format.
func (f DataFormat) String() string { return f.value }

// ProviderID names a secret provider. The set is open: an engine advertises the providers it
// supports, and the model only checks the spelling.
type ProviderID struct{ value string }

// ParseProviderID returns s as a ProviderID, or an *Error with code structure.invalid_value.
func ParseProviderID(s string) (ProviderID, error) {
	if err := checkName("secret provider", s, kebabName, providerIDMax); err != nil {
		return ProviderID{}, err
	}
	return ProviderID{value: s}, nil
}

// String returns the identifier.
func (p ProviderID) String() string { return p.value }
