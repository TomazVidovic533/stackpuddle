package model

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

// duplicates reports every declaration after the first of the same identity, at the later one,
// with the earlier one related. Declarations arrive in root-then-imports order.
func duplicates[T any](items []located[T], identity func(T) string) Diagnostics {
	var diagnostics Diagnostics
	first := map[string]origin{}
	for _, item := range items {
		id := identity(item.value)
		if earlier, seen := first[id]; seen {
			diagnostics = append(diagnostics, newDiagnostic(IdentityDuplicateDeclaration, item.at.location(""),
				fmt.Sprintf("%s is declared more than once", id), earlier.location("")))
			continue
		}
		first[id] = item.at
	}
	return diagnostics
}

func (l *loader) checkIdentity() Diagnostics {
	a := l.assembly
	diagnostics := slices.Concat(
		duplicates(a.definitions, func(d localDefinition) string { return "definition " + strconv.Quote(d.name.value) }),
		duplicates(a.inputs, func(i Input) string { return "input " + strconv.Quote(i.key.value) }),
		duplicates(a.workloads, func(w Workload) string { return refOf(ObjectRef{kind: ObjectWorkload, key: w.key}).text }),
		duplicates(a.modules, func(m ModuleInstance) string { return refOf(m.ref()).text }),
		duplicates(a.clients, func(c Client) string { return refOf(ObjectRef{kind: ObjectClient, key: c.key}).text }),
		duplicates(a.applications, func(app Application) string { return appRef(app.key).text }),
		duplicates(a.environments, func(e Environment) string { return envRef(e.name).text }),
	)
	for _, environment := range a.environments {
		first := map[ExportRef]origin{}
		for index, exposure := range environment.value.exposures {
			at := environment.at.child("/exposures/" + strconv.Itoa(index))
			if earlier, seen := first[exposure.Source]; seen {
				diagnostics = append(diagnostics, newDiagnostic(IdentityDuplicateDeclaration, at.location(""),
					fmt.Sprintf("export %q of %s is exposed more than once", exposure.Source.Export, exposure.Source.Object), earlier.location("")))
				continue
			}
			first[exposure.Source] = at
		}
	}
	for _, entry := range a.definitions {
		if packaged := slices.ContainsFunc(l.packaged, func(d ModuleDefinition) bool { return d.name == entry.value.name }); packaged {
			diagnostics = append(diagnostics, newDiagnostic(IdentityDuplicateDeclaration, entry.at.location(""),
				fmt.Sprintf("local definition %q has the name of a supplied packaged definition", entry.value.name)))
		}
		if loaded, ok := l.local[entry.value.name]; ok && loaded.name != entry.value.name {
			diagnostics = append(diagnostics, newDiagnostic(IdentityDefinitionNameMismatch, entry.at.location(""),
				fmt.Sprintf("the definition declared as %q is named %q in its module.yaml", entry.value.name, loaded.name),
				loaded.descriptor.at("/name")))
		}
	}
	return diagnostics
}

// lookup is the outcome of resolving a reference.
type lookup int

const (
	lookupFound lookup = iota + 1
	lookupNoObject
	lookupNoMember
	lookupUnresolved
)

func (l *loader) workload(key ObjectKey) (Workload, bool) {
	item, ok := l.declared.workloads[key]
	return item.value, ok
}

func (l *loader) module(key ObjectKey) (ModuleInstance, bool) {
	item, ok := l.declared.modules[key]
	return item.value, ok
}

func (l *loader) objectExists(ref ObjectRef) bool {
	switch ref.kind {
	case ObjectWorkload:
		_, ok := l.declared.workloads[ref.key]
		return ok
	case ObjectModule:
		_, ok := l.declared.modules[ref.key]
		return ok
	case ObjectClient:
		_, ok := l.declared.clients[ref.key]
		return ok
	default:
		return false
	}
}

func (l *loader) instanceDefinition(key ObjectKey) (ModuleDefinition, lookup) {
	instance, ok := l.module(key)
	if !ok {
		return ModuleDefinition{}, lookupNoObject
	}
	definition, resolved := l.resolved[instance.definition]
	if !resolved {
		return ModuleDefinition{}, lookupUnresolved
	}
	return definition, lookupFound
}

// exportOf resolves an Export reference against workloads and module instances; clients export
// nothing.
func (l *loader) exportOf(ref ExportRef) (Export, lookup) {
	switch ref.Object.kind {
	case ObjectWorkload:
		workload, ok := l.workload(ref.Object.key)
		if !ok {
			return Export{}, lookupNoObject
		}
		export, ok := workload.export(ref.Export)
		return export, foundOr(ok, lookupNoMember)
	case ObjectModule:
		definition, status := l.instanceDefinition(ref.Object.key)
		if status != lookupFound {
			return Export{}, status
		}
		export, ok := definition.export(ref.Export)
		return export.Export, foundOr(ok, lookupNoMember)
	default:
		if !l.objectExists(ref.Object) {
			return Export{}, lookupNoObject
		}
		return Export{}, lookupNoMember
	}
}

// dependencyOf resolves the Dependency a binding satisfies.
func (l *loader) dependencyOf(ref BindingRef) (Dependency, lookup) {
	switch ref.Consumer.kind {
	case ObjectWorkload:
		workload, ok := l.workload(ref.Consumer.key)
		if !ok {
			return Dependency{}, lookupNoObject
		}
		dependency, ok := workload.dependency(ref.Dependency)
		return dependency, foundOr(ok, lookupNoMember)
	case ObjectModule:
		definition, status := l.instanceDefinition(ref.Consumer.key)
		if status != lookupFound {
			return Dependency{}, status
		}
		_, dependency, ok := definition.dependencyWorkload(ref.Dependency)
		return dependency, foundOr(ok, lookupNoMember)
	default:
		client, ok := l.declared.clients[ref.Consumer.key]
		if !ok {
			return Dependency{}, lookupNoObject
		}
		index := slices.IndexFunc(client.value.dependencies, func(d Dependency) bool { return d.Key == ref.Dependency })
		if index < 0 {
			return Dependency{}, lookupNoMember
		}
		return client.value.dependencies[index], lookupFound
	}
}

// foundOr returns lookupFound when ok, else fallback.
func foundOr(ok bool, fallback lookup) lookup {
	if ok {
		return lookupFound
	}
	return fallback
}

func (l *loader) inputExists(key InputKey) bool {
	_, ok := l.declared.inputs[key]
	return ok
}

func (l *loader) checkReferences() Diagnostics {
	diagnostics := l.resolveDefinitions()
	scope := workloadScope{workload: l.workload, input: l.inputExists, fsys: l.fsys, buildDir: l.dir}
	for _, workload := range l.assembly.workloads {
		diagnostics = append(diagnostics, scope.check(workload)...)
	}
	for _, application := range l.assembly.applications {
		for index, member := range application.value.workloads {
			if _, ok := l.workload(member); !ok {
				diagnostics = append(diagnostics, unknownObject(application.at.location("/workloads/"+strconv.Itoa(index)), "workload/"+member.value))
			}
		}
	}
	for _, binding := range l.assembly.bindings {
		diagnostics = append(diagnostics, l.bindingReferences(binding)...)
	}
	for _, environment := range l.assembly.environments {
		diagnostics = append(diagnostics, l.environmentReferences(environment)...)
	}
	return append(diagnostics, l.provenanceReferences()...)
}

func unknownObject(at Location, ref string) Diagnostic {
	return newDiagnostic(ReferenceUnknownObject, at, fmt.Sprintf("%s is not declared", ref))
}

// resolveDefinitions decides each instance's definition: local, else supplied packaged, else
// unresolved. With a Manifest a non-local definition must be listed; a listed one nobody supplied is
// recorded as unresolved and every check that needs it is skipped.
func (l *loader) resolveDefinitions() Diagnostics {
	var diagnostics Diagnostics
	l.resolved = map[ModuleName]ModuleDefinition{}
	manifest, hasManifest := l.assembly.manifest.get()
	listed := make(map[ModuleName]bool, len(manifest.modules))
	for _, name := range manifest.modules {
		listed[name] = true
	}
	unresolved := map[ModuleName]bool{}
	for _, instance := range l.assembly.modules {
		name := instance.value.definition
		if definition, ok := l.local[name]; ok {
			l.resolved[name] = definition
			continue
		}
		definition, supplied := l.definition(name)
		at := instance.spec.location("/definition")
		switch {
		case hasManifest && !listed[name]:
			diagnostics = append(diagnostics, newDiagnostic(ReferenceModuleNotInManifest, at,
				fmt.Sprintf("module %q is neither local nor listed in the Manifest", name)))
		case supplied:
			l.resolved[name] = definition
		case hasManifest:
			if !unresolved[name] {
				unresolved[name] = true
				l.unresolved = append(l.unresolved, name)
			}
		default:
			diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownDefinition, at,
				fmt.Sprintf("definition %q is neither local nor supplied", name)))
		}
	}
	slices.SortFunc(l.unresolved, func(a, b ModuleName) int { return compareText(a.value, b.value) })
	return diagnostics
}

// workloadScope checks a workload's references within the scope that declares it: a project or a
// Module Definition.
type workloadScope struct {
	workload func(ObjectKey) (Workload, bool)
	input    func(InputKey) bool
	fsys     fs.FS
	buildDir string
}

func (s workloadScope) check(item located[Workload]) Diagnostics {
	var diagnostics Diagnostics
	workload := item.value
	if build, isBuild := workload.artifact.(BuildArtifact); isBuild {
		info, err := fs.Stat(s.fsys, path.Join(s.buildDir, build.context))
		if err != nil || !info.IsDir() {
			diagnostics = append(diagnostics, newDiagnostic(ReferencePathNotFound, item.spec.location("/build/context"),
				fmt.Sprintf("build context %q is not a directory", build.context)))
		}
	}
	for _, export := range workload.exports {
		if !slices.ContainsFunc(workload.listeners, func(listener Listener) bool { return listener.Key == export.Listener }) {
			diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownListener,
				item.spec.location(joinPointer("/exports", export.Key.value)+"/listener"),
				fmt.Sprintf("export %q names listener %q, which the workload does not declare", export.Key, export.Listener)))
		}
	}
	for index, key := range workload.prerequisites {
		at := item.spec.location("/prerequisites/" + strconv.Itoa(index) + "/job")
		target, ok := s.workload(key)
		switch {
		case !ok:
			diagnostics = append(diagnostics, unknownObject(at, "workload/"+key.value))
		case target.workloadType != WorkloadJob:
			diagnostics = append(diagnostics, newDiagnostic(ReferenceWrongKind, at,
				fmt.Sprintf("prerequisite %q is a service; prerequisites must be jobs", key)))
		}
	}
	diagnostics = append(diagnostics, inputReferences(workload.environment, item.spec.child("/environment"), s.input)...)
	return diagnostics
}

func inputReferences(variables []EnvVar, at origin, exists func(InputKey) bool) Diagnostics {
	var diagnostics Diagnostics
	for _, variable := range variables {
		if input, isInput := variable.Value.(InputValue); isInput && !exists(input.input) {
			diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownInput, at.location(joinPointer("", variable.Key.value)),
				fmt.Sprintf("input %q is not declared", input.input)))
		}
	}
	return diagnostics
}

func (l *loader) bindingReferences(item located[Binding]) Diagnostics {
	binding := item.value
	if !l.objectExists(binding.ref.Consumer) {
		return Diagnostics{unknownObject(item.at.location("/consumer"), binding.ref.Consumer.String())}
	}
	var diagnostics Diagnostics
	if _, status := l.dependencyOf(binding.ref); status == lookupNoMember {
		diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownDependency, item.at.location("/dependency"),
			fmt.Sprintf("%s declares no dependency %q", binding.ref.Consumer, binding.ref.Dependency)))
	}
	export, status := l.exportOf(binding.source)
	switch status {
	case lookupNoObject:
		return append(diagnostics, unknownObject(item.at.location("/source/object"), binding.source.Object.String()))
	case lookupNoMember:
		return append(diagnostics, unknownExport(item.at.location("/source/export"), binding.source))
	case lookupUnresolved:
		return diagnostics
	}
	delivery, hasDelivery := binding.delivery.get()
	if !hasDelivery {
		return diagnostics
	}
	for _, variable := range delivery.environment {
		if !export.offers(variable.Value) {
			diagnostics = append(diagnostics, unknownValue(item.at.location(joinPointer("/delivery/environment", variable.Key.value)), export, variable.Value))
		}
	}
	for _, file := range delivery.files {
		if !export.offers(file.Value) {
			diagnostics = append(diagnostics, unknownValue(item.at.location(joinPointer("/delivery/files", file.Value.value)), export, file.Value))
		}
	}
	return diagnostics
}

func unknownExport(at Location, ref ExportRef) Diagnostic {
	return newDiagnostic(ReferenceUnknownExport, at, fmt.Sprintf("%s offers no export %q", ref.Object, ref.Export))
}

func unknownValue(at Location, export Export, name ValueName) Diagnostic {
	return newDiagnostic(ReferenceUnknownValue, at, fmt.Sprintf("export %q offers no value %q", export.Key, name))
}

func (l *loader) environmentReferences(item located[Environment]) Diagnostics {
	var diagnostics Diagnostics
	environment := item.value
	for index, exposure := range environment.exposures {
		at := item.at.child("/exposures/" + strconv.Itoa(index) + "/source")
		switch _, status := l.exportOf(exposure.Source); status {
		case lookupNoObject:
			diagnostics = append(diagnostics, unknownObject(at.location("/object"), exposure.Source.Object.String()))
		case lookupNoMember:
			diagnostics = append(diagnostics, unknownExport(at.location("/export"), exposure.Source))
		}
	}
	for _, sources := range environment.inputSources {
		if !l.inputExists(sources.Key) {
			diagnostics = append(diagnostics, newDiagnostic(ReferenceUnknownInput, item.at.location(joinPointer("/inputs", sources.Key.value)),
				fmt.Sprintf("input %q is not declared", sources.Key)))
		}
	}
	for _, override := range environment.workloadOverrides {
		overrideAt := item.at.child(joinPointer("/overrides/workloads", override.Key.value))
		if _, ok := l.workload(override.Key); !ok {
			diagnostics = append(diagnostics, unknownObject(overrideAt.location(""), "workload/"+override.Key.value))
			continue
		}
		diagnostics = append(diagnostics, inputReferences(override.Environment, overrideAt.child("/environment"), l.inputExists)...)
	}
	for _, override := range environment.moduleOverrides {
		if _, ok := l.module(override.Key); !ok {
			diagnostics = append(diagnostics, unknownObject(item.at.location(joinPointer("/overrides/modules", override.Key.value)), "module/"+override.Key.value))
		}
	}
	return diagnostics
}

// provenanceReferences checks that a Model's provenance names only declared things.
func (l *loader) provenanceReferences() Diagnostics {
	if l.root.kind != kindModel {
		return nil
	}
	declared := map[Ref]bool{}
	for _, workload := range l.assembly.workloads {
		declared[refOf(ObjectRef{kind: ObjectWorkload, key: workload.value.key})] = true
	}
	for _, instance := range l.assembly.modules {
		declared[refOf(instance.value.ref())] = true
	}
	for _, client := range l.assembly.clients {
		declared[refOf(ObjectRef{kind: ObjectClient, key: client.value.key})] = true
	}
	for _, application := range l.assembly.applications {
		declared[appRef(application.value.key)] = true
	}
	for _, environment := range l.assembly.environments {
		declared[envRef(environment.value.name)] = true
	}
	for _, binding := range l.assembly.bindings {
		declared[bindingRef(binding.value.ref)] = true
	}
	var diagnostics Diagnostics
	for ref, at := range l.assembly.provenanceAt {
		if !declared[ref] {
			diagnostics = append(diagnostics, unknownObject(at.location(""), ref.text))
		}
	}
	return diagnostics
}

// checkBindings runs the binding stage: identity of bindings, capability match, delivery rules and
// unbound Dependencies.
func (l *loader) checkBindings() Diagnostics {
	var diagnostics Diagnostics
	first := map[BindingRef]located[Binding]{}
	for _, item := range l.assembly.bindings {
		binding := item.value
		if earlier, seen := first[binding.ref]; seen {
			code, message := BindingDuplicate, "%s is declared twice with the same source"
			if earlier.value.source != binding.source {
				code, message = BindingConflict, "%s is declared twice with different sources"
			}
			diagnostics = append(diagnostics, newDiagnostic(code, item.at.location(""), fmt.Sprintf(message, binding.ref), earlier.at.location("")))
			continue
		}
		first[binding.ref] = item
		dependency, dependencyFound := l.dependencyOf(binding.ref)
		export, exportFound := l.exportOf(binding.source)
		if dependencyFound == lookupFound && exportFound == lookupFound && dependency.Capability != export.Capability {
			diagnostics = append(diagnostics, newDiagnostic(BindingCapabilityMismatch, item.at.location("/source"),
				fmt.Sprintf("dependency %q needs %s but export %q offers %s", binding.ref.Dependency, dependency.Capability, export.Key, export.Capability)))
		}
		if _, hasDelivery := binding.delivery.get(); hasDelivery && binding.ref.Consumer.kind == ObjectClient {
			diagnostics = append(diagnostics, newDiagnostic(BindingDeliveryUnsupported, item.at.location("/delivery"),
				"a client runs outside StackPuddle, so nothing can deliver values to it"))
		}
	}
	diagnostics = append(diagnostics, l.deliveryConflicts()...)
	return append(diagnostics, l.unboundDependencies(first)...)
}

// deliveryTarget is the workload a delivery writes into: the consumer workload, or the contributed
// workload of a module instance that declares the Dependency.
type deliveryTarget struct {
	consumer ObjectRef
	workload ObjectKey
}

// deliveryConflicts refuses two configuration paths for one variable or file: two deliveries, or a
// delivery and a declared or overridden environment variable.
func (l *loader) deliveryConflicts() Diagnostics {
	claims := map[deliveryTarget]map[string]Location{}
	overrides := l.overridesByWorkload()
	claimsFor := func(target deliveryTarget) map[string]Location {
		if existing, ok := claims[target]; ok {
			return existing
		}
		created := l.declaredClaims(target, overrides)
		claims[target] = created
		return created
	}
	var diagnostics Diagnostics
	for _, item := range l.assembly.bindings {
		delivery, hasDelivery := item.value.delivery.get()
		target, ok := l.deliveryTarget(item.value.ref)
		if !hasDelivery || !ok {
			continue
		}
		taken := claimsFor(target)
		for _, variable := range delivery.environment {
			at := item.at.location(joinPointer("/delivery/environment", variable.Key.value))
			diagnostics = append(diagnostics, claim(taken, "env:"+variable.Key.value, at, "environment variable "+strconv.Quote(variable.Key.value))...)
		}
		for _, file := range delivery.files {
			at := item.at.location(joinPointer("/delivery/files", file.Value.value) + "/path")
			diagnostics = append(diagnostics, claim(taken, "file:"+file.Path, at, "file "+strconv.Quote(file.Path))...)
		}
	}
	return diagnostics
}

func claim(taken map[string]Location, key string, at Location, what string) Diagnostics {
	if earlier, ok := taken[key]; ok {
		return Diagnostics{newDiagnostic(BindingDeliveryConflict, at, fmt.Sprintf("%s is already set by another configuration path", what), earlier)}
	}
	taken[key] = at
	return nil
}

func (l *loader) deliveryTarget(ref BindingRef) (deliveryTarget, bool) {
	switch ref.Consumer.kind {
	case ObjectWorkload:
		return deliveryTarget{consumer: ref.Consumer, workload: ref.Consumer.key}, true
	case ObjectModule:
		definition, status := l.instanceDefinition(ref.Consumer.key)
		if status != lookupFound {
			return deliveryTarget{}, false
		}
		workload, _, ok := definition.dependencyWorkload(ref.Dependency)
		return deliveryTarget{consumer: ref.Consumer, workload: workload.key}, ok
	default:
		return deliveryTarget{}, false
	}
}

// overridesByWorkload returns every environment's workload overrides by workload key, in
// declaration order, each located at its environment member.
func (l *loader) overridesByWorkload() map[ObjectKey][]located[WorkloadOverride] {
	overrides := map[ObjectKey][]located[WorkloadOverride]{}
	for _, environment := range l.assembly.environments {
		for _, override := range environment.value.workloadOverrides {
			at := environment.at.child(joinPointer("/overrides/workloads", override.Key.value) + "/environment")
			overrides[override.Key] = append(overrides[override.Key], locatedAt(override, at))
		}
	}
	return overrides
}

// declaredClaims returns the environment variables a target workload already sets: its declared
// variables and, for a project workload, every environment's overrides.
func (l *loader) declaredClaims(target deliveryTarget, overrides map[ObjectKey][]located[WorkloadOverride]) map[string]Location {
	taken := map[string]Location{}
	if target.consumer.kind == ObjectModule {
		definition, _ := l.instanceDefinition(target.consumer.key)
		workload, _ := definition.workload(target.workload)
		at := origin{doc: definition.descriptor, pointer: joinPointer("/workloads", workload.key.value) + "/environment"}
		for _, variable := range workload.environment {
			taken["env:"+variable.Key.value] = at.location(joinPointer("", variable.Key.value))
		}
		return taken
	}
	if item, ok := l.declared.workloads[target.workload]; ok {
		for _, variable := range item.value.environment {
			taken["env:"+variable.Key.value] = item.spec.location(joinPointer("/environment", variable.Key.value))
		}
	}
	for _, override := range overrides[target.workload] {
		for _, variable := range override.value.Environment {
			if _, declared := taken["env:"+variable.Key.value]; !declared {
				taken["env:"+variable.Key.value] = override.at.location(joinPointer("", variable.Key.value))
			}
		}
	}
	return taken
}

// unboundDependencies reports every Dependency no binding satisfies.
func (l *loader) unboundDependencies(bound map[BindingRef]located[Binding]) Diagnostics {
	var diagnostics Diagnostics
	report := func(consumer ObjectRef, dependency Dependency, at Location) {
		if _, ok := bound[BindingRef{Consumer: consumer, Dependency: dependency.Key}]; !ok {
			diagnostics = append(diagnostics, newDiagnostic(BindingUnboundDependency, at,
				fmt.Sprintf("dependency %q of %s has no binding", dependency.Key, consumer)))
		}
	}
	for _, workload := range l.assembly.workloads {
		consumer := ObjectRef{kind: ObjectWorkload, key: workload.value.key}
		for _, dependency := range workload.value.dependencies {
			report(consumer, dependency, workload.spec.location(joinPointer("/dependencies", dependency.Key.value)))
		}
	}
	for _, client := range l.assembly.clients {
		consumer := ObjectRef{kind: ObjectClient, key: client.value.key}
		for _, dependency := range client.value.dependencies {
			report(consumer, dependency, client.spec.location(joinPointer("/dependencies", dependency.Key.value)))
		}
	}
	for _, instance := range l.assembly.modules {
		definition, ok := l.resolved[instance.value.definition]
		if !ok {
			continue
		}
		for _, dependency := range definition.Dependencies() {
			report(instance.value.ref(), dependency, instance.spec.location("/definition"))
		}
	}
	return diagnostics
}

func (l *loader) checkGraph() Diagnostics {
	return prerequisiteCycles(l.assembly.workloads)
}

// prerequisiteCycles reports each cycle of job prerequisites once, at its smallest key, with the
// other members related.
func prerequisiteCycles(workloads []located[Workload]) Diagnostics {
	graph := cycleFinder{workloads: map[ObjectKey]located[Workload]{}, index: map[ObjectKey]int{}, low: map[ObjectKey]int{}, onStack: map[ObjectKey]bool{}}
	for _, workload := range workloads {
		graph.workloads[workload.value.key] = workload
	}
	keys := make([]ObjectKey, 0, len(graph.workloads))
	for key := range graph.workloads {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b ObjectKey) int { return compareText(a.value, b.value) })
	for _, key := range keys {
		if _, visited := graph.index[key]; !visited {
			graph.visit(key)
		}
	}
	var diagnostics Diagnostics
	for _, component := range graph.cycles {
		slices.SortFunc(component, func(a, b ObjectKey) int { return compareText(a.value, b.value) })
		names := make([]string, len(component))
		related := make([]Location, 0, len(component)-1)
		for index, key := range component {
			names[index] = strconv.Quote(key.value)
			if index > 0 {
				related = append(related, graph.workloads[key].spec.location("/prerequisites"))
			}
		}
		diagnostics = append(diagnostics, newDiagnostic(GraphPrerequisiteCycle, graph.workloads[component[0]].spec.location("/prerequisites"),
			fmt.Sprintf("jobs %s form a prerequisite cycle", strings.Join(names, ", ")), related...))
	}
	return diagnostics
}

// cycleFinder finds strongly connected components (Tarjan) of the prerequisite graph.
type cycleFinder struct {
	workloads map[ObjectKey]located[Workload]
	index     map[ObjectKey]int
	low       map[ObjectKey]int
	onStack   map[ObjectKey]bool
	stack     []ObjectKey
	next      int
	cycles    [][]ObjectKey
}

func (g *cycleFinder) visit(key ObjectKey) {
	g.index[key], g.low[key] = g.next, g.next
	g.next++
	g.stack = append(g.stack, key)
	g.onStack[key] = true
	selfLoop := false
	for _, prerequisite := range g.workloads[key].value.prerequisites {
		if _, declared := g.workloads[prerequisite]; !declared {
			continue
		}
		selfLoop = selfLoop || prerequisite == key
		if _, visited := g.index[prerequisite]; !visited {
			g.visit(prerequisite)
			g.low[key] = min(g.low[key], g.low[prerequisite])
		} else if g.onStack[prerequisite] {
			g.low[key] = min(g.low[key], g.index[prerequisite])
		}
	}
	if g.low[key] != g.index[key] {
		return
	}
	var component []ObjectKey
	for {
		top := g.stack[len(g.stack)-1]
		g.stack = g.stack[:len(g.stack)-1]
		g.onStack[top] = false
		component = append(component, top)
		if top == key {
			break
		}
	}
	if len(component) > 1 || selfLoop {
		g.cycles = append(g.cycles, component)
	}
}
