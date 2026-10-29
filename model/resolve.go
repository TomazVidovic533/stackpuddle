package model

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

// Context is where a consumer runs, which decides the address it reaches an Export at.
type Context int

const (
	// ContextContainer is a workload or module instance on the environment's runtime network.
	ContextContainer Context = iota + 1
	// ContextHost is a client on the developer's machine, which reaches only exposed Exports.
	ContextHost
)

// String returns "container" or "host".
func (c Context) String() string {
	switch c {
	case ContextContainer:
		return "container"
	case ContextHost:
		return "host"
	default:
		return "unknown"
	}
}

// Endpoint is an address a consumer reaches an Export at.
type Endpoint struct {
	Host string
	Port int
}

const (
	maxHostLength = 253
	maxPort       = 65535
)

func (e Endpoint) valid() bool {
	return e.Host != "" && len(e.Host) <= maxHostLength && e.Port >= 1 && e.Port <= maxPort
}

// AddressLookup returns the address of source as seen from context, or false when the runtime has
// none. The runtime adapter supplies it; the model never invents an address.
type AddressLookup func(source ExportRef, context Context) (Endpoint, bool)

// Bindings is the resolved bindings of one project environment.
type Bindings struct {
	project     ProjectName
	environment EnvironmentName
	entries     []ResolvedBinding
}

// Project returns the project name.
func (b Bindings) Project() ProjectName { return b.project }

// Environment returns the environment name.
func (b Bindings) Environment() EnvironmentName { return b.environment }

// Entries returns the resolved bindings, ordered by binding reference.
func (b Bindings) Entries() []ResolvedBinding { return slices.Clone(b.entries) }

// ResolvedBinding is one binding with the endpoint its consumer reaches.
type ResolvedBinding struct {
	Ref        BindingRef
	Source     ExportRef
	Capability Capability
	Context    Context
	Endpoint   Endpoint
}

// ResolveBindings resolves every binding of the project in the named environment. Each consumer's
// context comes from its kind: a client needs its source exposed on the host, a container consumer
// reaches it on the runtime network. lookup supplies each address. Unknown environments, unexposed
// sources for host consumers (binding.unreachable_context) and addresses lookup lacks
// (binding.address_unavailable) are diagnostics; then the Bindings value is the zero value.
func (p Project) ResolveBindings(env EnvironmentName, lookup AddressLookup) (Bindings, Diagnostics) {
	environment, ok := p.Environment(env)
	if !ok {
		return Bindings{}, Diagnostics{unknownObject(NewLocation("", joinPointer("/spec/environments", env.value), 0, 0), envRef(env).text)}
	}
	var diagnostics Diagnostics
	resolved := Bindings{project: p.name, environment: env}
	for _, binding := range p.bindings {
		export, status := p.exportOf(binding.source)
		switch status {
		case lookupUnresolved:
			diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownDefinition, binding.at.location("/source"),
				fmt.Sprintf("the definition behind %s is not resolved, so its export has no known capability", binding.source.Object)))
			continue
		case lookupNoObject:
			diagnostics = append(diagnostics, unknownObject(binding.at.location("/source/object"), binding.source.Object.String()))
			continue
		case lookupNoMember:
			diagnostics = append(diagnostics, unknownExport(binding.at.location("/source/export"), binding.source))
			continue
		}
		context := binding.ref.Consumer.kind.context()
		if context == ContextHost && !environment.exposes(binding.source) {
			diagnostics = append(diagnostics, newDiagnostic(BindingUnreachableContext, binding.at.location(""),
				fmt.Sprintf("%s runs on the host but environment %q does not expose %s export %q", binding.ref.Consumer, env, binding.source.Object, binding.source.Export)))
			continue
		}
		endpoint, found := lookup(binding.source, context)
		if !found || !endpoint.valid() {
			diagnostics = append(diagnostics, newDiagnostic(BindingAddressUnavailable, binding.at.location(""),
				fmt.Sprintf("the runtime supplies no %s address for %s export %q", context, binding.source.Object, binding.source.Export)))
			continue
		}
		resolved.entries = append(resolved.entries, ResolvedBinding{
			Ref: binding.ref, Source: binding.source, Capability: export.Capability, Context: context, Endpoint: endpoint,
		})
	}
	if len(diagnostics) > 0 {
		return Bindings{}, diagnostics.Sorted()
	}
	return resolved, nil
}

func (p Project) exportOf(ref ExportRef) (Export, lookup) {
	switch ref.Object.kind {
	case ObjectWorkload:
		workload, ok := p.workload(ref.Object.key)
		if !ok {
			return Export{}, lookupNoObject
		}
		export, ok := workload.export(ref.Export)
		return export, foundOr(ok, lookupNoMember)
	case ObjectModule:
		instance, ok := p.module(ref.Object.key)
		if !ok {
			return Export{}, lookupNoObject
		}
		definition, ok := p.Definition(instance.definition)
		if !ok {
			return Export{}, lookupUnresolved
		}
		export, ok := definition.export(ref.Export)
		return export.Export, foundOr(ok, lookupNoMember)
	default:
		return Export{}, lookupNoMember
	}
}

// EncodeBindings returns the canonical Bindings document of b.
func EncodeBindings(b Bindings) []byte {
	entries := canonical.Array{}
	for _, entry := range b.entries {
		entries = append(entries, canonical.Object{
			"consumer":   canonical.String(entry.Ref.Consumer.String()),
			"dependency": canonical.String(entry.Ref.Dependency.value),
			"source":     encodeExportRef(entry.Source),
			"capability": canonical.String(entry.Capability.value),
			"context":    canonical.String(entry.Context.String()),
			"endpoint": canonical.Object{
				"host": canonical.String(entry.Endpoint.Host), "port": canonical.Number(strconv.Itoa(entry.Endpoint.Port)),
			},
		})
	}
	return canonical.Encode(canonical.Object{
		"apiVersion": canonical.String(APIVersion),
		"kind":       canonical.String(kindBindings),
		"metadata":   canonical.Object{"project": canonical.String(b.project.value), "environment": canonical.String(b.environment.value)},
		"bindings":   entries,
	})
}
