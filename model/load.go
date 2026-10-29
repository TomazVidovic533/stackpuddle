package model

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
)

// located is a declaration and where it sits. at is the declaring member; spec is where its fields
// are, which differs from at only for the object envelopes of a Model file.
type located[T any] struct {
	value T
	at    origin
	spec  origin
}

func locatedAt[T any](value T, at origin) located[T] {
	return located[T]{value: value, at: at, spec: at}
}

// Project limits bound the work one Load causes beyond what the per-file limits bound: how many files
// it reads, and how much the environment override checks walk.
const (
	// maxDefinitionDirs caps the directories one project loads local definitions from. Each reads up
	// to three files, so with the per-file limits it bounds the files and bytes one Load reads.
	maxDefinitionDirs = 16
	// maxOverrideValues is a named default, 262,144 option values, that caps what the environment
	// override checks of one project walk. Each check walks its instance's options merged with the
	// override, so without the cap the work would grow with overrides times options while the files
	// stay small. A project that needs more splits its environments; the number is not configurable.
	maxOverrideValues = 1 << 18
	// maxOptionCheckUnits is the evaluation budget of one Load (and of one ValidateOptions call):
	// each options check is charged its instance's value count times the option schema's size with
	// references expanded, and the check that would pass the budget is refused with
	// syntax.limit_exceeded at its instance. Validation visits each pair of schema value and
	// instance value at most once per failing branch, so this bounds the work whatever the schema's
	// anyOf and oneOf branches do; about 600 ns a unit measured, so a few seconds at the limit.
	maxOptionCheckUnits = 1 << 23
)

// Load reads the project whose root file is path in fsys: a Project with the Fragment files it
// imports, or a canonical Model file. Relative paths in the files resolve against the root file's
// directory. packaged supplies the Module Definitions of packaged modules; the first one of each
// name is used. Pass an os.Root-backed file system for untrusted trees: Load reads only through
// fsys and nothing else.
//
// Load runs fixed stages (syntax, compatibility, structure, identity, reference, binding and graph)
// over every file, and stops after the first stage that reports anything, so each diagnostic names
// a root cause rather than its consequences. On any diagnostic the Project is the zero value.
// Load provisions nothing and reads no environment, clock or network.
func Load(fsys fs.FS, path string, packaged []ModuleDefinition) (Project, Diagnostics) {
	reader, diagnostics := newReader(fsys)
	if len(diagnostics) > 0 {
		return Project{}, diagnostics
	}
	l := &loader{reader: reader, packaged: packaged, local: map[ModuleName]ModuleDefinition{}}
	project, diagnostics := l.load(path)
	if len(diagnostics) > 0 {
		return Project{}, diagnostics.Sorted()
	}
	return project, nil
}

type loader struct {
	*reader
	packaged   []ModuleDefinition
	dir        string
	root       *document
	project    wireProject
	model      wireModel
	imports    []*document
	manifest   *document
	lock       *document
	assembly   assembly
	declared   declarations
	local      map[ModuleName]ModuleDefinition
	resolved   map[ModuleName]ModuleDefinition
	unresolved []ModuleName
	failed     map[ObjectKey]bool
	// overrideValues is what the environment override checks may still walk.
	overrideValues int
	// checkUnits is what the options checks may still be charged; see maxOptionCheckUnits.
	checkUnits int
	// optionValues holds each instance's option value count, counted once.
	optionValues map[ObjectKey]int
}

func (l *loader) load(rootPath string) (Project, Diagnostics) {
	stages := []func() Diagnostics{
		func() Diagnostics { return l.readRoot(rootPath) },
		l.readReferenced,
		l.assemble,
		l.loadLocalDefinitions,
		l.checkStructure,
		l.checkIdentity,
		l.checkReferences,
		l.checkBindings,
		l.checkGraph,
	}
	for _, stage := range stages {
		if diagnostics := stage(); len(diagnostics) > 0 {
			return Project{}, diagnostics
		}
	}
	return l.build(), nil
}

func (l *loader) readRoot(rootPath string) Diagnostics {
	l.dir = path.Dir(rootPath)
	root, diagnostics := l.read(rootPath, optional[Location]{})
	if len(diagnostics) > 0 {
		return diagnostics
	}
	if diagnostics := compatibility(root); len(diagnostics) > 0 {
		return diagnostics
	}
	if diagnostics := l.structure(root, kindProject, kindModel); len(diagnostics) > 0 {
		return diagnostics
	}
	l.root = root
	if root.kind == kindModel {
		return root.decode(&l.model)
	}
	return root.decode(&l.project)
}

type reference struct {
	pointer string
	file    string
	kind    string
	target  **document
}

// readReferenced runs syntax, compatibility and structure over every file the root names, one
// stage at a time across all of them.
func (l *loader) readReferenced() Diagnostics {
	var references []reference
	manifest, lock := l.project.Spec.Manifest, l.project.Spec.Lock
	if l.root.kind == kindModel {
		manifest, lock = l.model.Spec.Manifest, l.model.Spec.Lock
	}
	l.imports = make([]*document, len(l.project.Spec.Imports))
	for index, file := range l.project.Spec.Imports {
		references = append(references, reference{
			pointer: "/spec/imports/" + strconv.Itoa(index), file: file, kind: kindFragment, target: &l.imports[index],
		})
	}
	if manifest != nil {
		references = append(references, reference{pointer: "/spec/manifest", file: *manifest, kind: kindManifest, target: &l.manifest})
	}
	if lock != nil {
		references = append(references, reference{pointer: "/spec/lock", file: *lock, kind: kindLock, target: &l.lock})
	}
	var diagnostics Diagnostics
	for _, ref := range references {
		doc, found := l.read(path.Join(l.dir, ref.file), some(l.root.at(ref.pointer)))
		diagnostics = append(diagnostics, found...)
		*ref.target = doc
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	for _, ref := range references {
		diagnostics = append(diagnostics, compatibility(*ref.target)...)
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	for _, ref := range references {
		diagnostics = append(diagnostics, l.structure(*ref.target, ref.kind)...)
	}
	return diagnostics
}

// assembly is every declaration of the project in root-then-imports order, before identity checks
// pick the one declaration of each name.
type assembly struct {
	name         ProjectName
	description  optional[string]
	manifestPath optional[string]
	lockPath     optional[string]
	manifest     optional[Manifest]
	lock         optional[Lock]
	definitions  []located[localDefinition]
	inputs       []located[Input]
	workloads    []located[Workload]
	modules      []located[ModuleInstance]
	clients      []located[Client]
	applications []located[Application]
	bindings     []located[Binding]
	environments []located[Environment]
	provenance   map[Ref]Provenance
	provenanceAt map[Ref]origin
}

func (l *loader) assemble() Diagnostics {
	convert := &converter{}
	a := &l.assembly
	a.provenance = map[Ref]Provenance{}
	a.provenanceAt = map[Ref]origin{}
	if l.root.kind == kindModel {
		l.assembleModel(convert)
	} else {
		a.name = convert.projectName(l.project.Metadata.Name)
		a.description = optionalOf(l.project.Metadata.Description)
		a.manifestPath = optionalOf(l.project.Spec.Manifest)
		a.lockPath = optionalOf(l.project.Spec.Lock)
		l.assembleSections(convert, l.root, l.project.Spec.wireSections)
		for _, doc := range l.imports {
			var fragment wireFragment
			if diagnostics := doc.decode(&fragment); len(diagnostics) > 0 {
				return diagnostics
			}
			l.assembleSections(convert, doc, fragment.Spec)
		}
	}
	if diagnostics := convert.diagnostics(l.root, ""); len(diagnostics) > 0 {
		return diagnostics
	}
	l.declared = indexDeclarations(a)
	return l.assembleManifestAndLock()
}

// declarations indexes the first declaration of each key, the one build keeps, so each lookup
// during the checks is one map read rather than a scan; identity reports every later declaration.
type declarations struct {
	workloads map[ObjectKey]located[Workload]
	modules   map[ObjectKey]located[ModuleInstance]
	clients   map[ObjectKey]located[Client]
	inputs    map[InputKey]located[Input]
}

func indexDeclarations(a *assembly) declarations {
	return declarations{
		workloads: firstByKey(a.workloads, func(w Workload) ObjectKey { return w.key }),
		modules:   firstByKey(a.modules, func(m ModuleInstance) ObjectKey { return m.key }),
		clients:   firstByKey(a.clients, func(c Client) ObjectKey { return c.key }),
		inputs:    firstByKey(a.inputs, func(i Input) InputKey { return i.key }),
	}
}

// firstByKey maps each key to the first of items that has it.
func firstByKey[K comparable, T any](items []located[T], keyOf func(T) K) map[K]located[T] {
	index := make(map[K]located[T], len(items))
	for _, item := range items {
		key := keyOf(item.value)
		if _, seen := index[key]; !seen {
			index[key] = item
		}
	}
	return index
}

func (l *loader) assembleManifestAndLock() Diagnostics {
	if l.manifest != nil {
		manifest, diagnostics := manifestFrom(l.manifest)
		if len(diagnostics) > 0 {
			return diagnostics
		}
		l.assembly.manifest = some(manifest)
	}
	if l.lock != nil {
		lock, diagnostics := lockFrom(l.lock)
		if len(diagnostics) > 0 {
			return diagnostics
		}
		l.assembly.lock = some(lock)
	}
	return nil
}

// assembleSections adds one Project or Fragment file's declarations, recording YAML provenance.
func (l *loader) assembleSections(convert *converter, doc *document, sections wireSections) {
	a := &l.assembly
	at := func(section, key string) origin { return origin{doc: doc, pointer: joinPointer("/spec/"+section, key)} }
	for _, name := range sortedKeys(sections.Definitions) {
		a.definitions = append(a.definitions, locatedAt(localDefinition{name: convert.moduleName(name), path: sections.Definitions[name].Path}, at("definitions", name)))
	}
	for _, key := range sortedKeys(sections.Inputs) {
		a.inputs = append(a.inputs, locatedAt(convert.input(key, sections.Inputs[key]), at("inputs", key)))
	}
	for _, key := range sortedKeys(sections.Workloads) {
		declaration := sections.Workloads[key]
		workload := convert.workload(key, declaration.wireWorkloadSpec, declaration.Metadata)
		a.workloads = append(a.workloads, locatedAt(workload, at("workloads", key)))
		l.recordYAML(refOf(ObjectRef{kind: ObjectWorkload, key: workload.key}), at("workloads", key))
	}
	for _, key := range sortedKeys(sections.Modules) {
		declaration := sections.Modules[key]
		instance := convert.moduleInstance(key, declaration.wireModuleSpec, declaration.Metadata, at("modules", key).child("/options"))
		a.modules = append(a.modules, locatedAt(instance, at("modules", key)))
		l.recordYAML(refOf(instance.ref()), at("modules", key))
	}
	for _, key := range sortedKeys(sections.Clients) {
		declaration := sections.Clients[key]
		client := convert.client(key, declaration.wireClientSpec, declaration.Metadata)
		a.clients = append(a.clients, locatedAt(client, at("clients", key)))
		l.recordYAML(refOf(ObjectRef{kind: ObjectClient, key: client.key}), at("clients", key))
	}
	for _, key := range sortedKeys(sections.Applications) {
		application := convert.application(key, sections.Applications[key])
		a.applications = append(a.applications, locatedAt(application, at("applications", key)))
		l.recordYAML(appRef(application.key), at("applications", key))
	}
	for index, wire := range sections.Bindings {
		bindingAt := origin{doc: doc, pointer: "/spec/bindings/" + strconv.Itoa(index)}
		binding := convert.binding(wire, bindingAt)
		a.bindings = append(a.bindings, locatedAt(binding, bindingAt))
		l.recordYAML(bindingRef(binding.ref), bindingAt)
	}
	for _, name := range sortedKeys(sections.Environments) {
		environment := convert.environment(name, sections.Environments[name])
		environment.moduleOverrideAt = overrideOrigins(environment, at("environments", name))
		a.environments = append(a.environments, locatedAt(environment, at("environments", name)))
		l.recordYAML(envRef(environment.name), at("environments", name))
	}
}

// recordYAML keeps the first declaration's position; identity reports any later one.
func (l *loader) recordYAML(ref Ref, at origin) {
	if _, seen := l.assembly.provenance[ref]; seen {
		return
	}
	declared := at.location("")
	line, column, _ := declared.Position()
	l.assembly.provenance[ref] = Provenance{declared: Origin{kind: OriginYAML, file: some(at.doc.file), line: line, column: column}}
	l.assembly.provenanceAt[ref] = at
}

// assembleModel adds the declarations of a canonical Model file and the provenance it carries.
func (l *loader) assembleModel(convert *converter) {
	a := &l.assembly
	spec := l.model.Spec
	a.name = convert.projectName(l.model.Metadata.Name)
	a.description = optionalOf(l.model.Metadata.Description)
	a.manifestPath = optionalOf(spec.Manifest)
	a.lockPath = optionalOf(spec.Lock)
	at := func(section, key string) origin {
		return origin{doc: l.root, pointer: joinPointer("/spec/"+section, key)}
	}
	for _, name := range sortedKeys(spec.Definitions) {
		a.definitions = append(a.definitions, locatedAt(localDefinition{name: convert.moduleName(name), path: spec.Definitions[name].Path}, at("definitions", name)))
	}
	for _, key := range sortedKeys(spec.Inputs) {
		a.inputs = append(a.inputs, locatedAt(convert.input(key, spec.Inputs[key]), at("inputs", key)))
	}
	for index, object := range spec.Objects {
		objectAt := origin{doc: l.root, pointer: "/spec/objects/" + strconv.Itoa(index)}
		l.assembleObject(convert, object, objectAt)
	}
	for _, key := range sortedKeys(spec.Applications) {
		a.applications = append(a.applications, locatedAt(convert.application(key, spec.Applications[key]), at("applications", key)))
	}
	for index, wire := range spec.Bindings {
		bindingAt := origin{doc: l.root, pointer: "/spec/bindings/" + strconv.Itoa(index)}
		a.bindings = append(a.bindings, locatedAt(convert.binding(wire, bindingAt), bindingAt))
	}
	for _, name := range sortedKeys(spec.Environments) {
		environment := convert.environment(name, spec.Environments[name])
		environment.moduleOverrideAt = overrideOrigins(environment, at("environments", name))
		a.environments = append(a.environments, locatedAt(environment, at("environments", name)))
	}
	for _, key := range sortedKeys(l.model.Provenance) {
		ref, err := ParseRef(key)
		convert.keep(err)
		a.provenance[ref] = convert.provenance(l.model.Provenance[key])
		a.provenanceAt[ref] = origin{doc: l.root, pointer: joinPointer("/provenance", key)}
	}
}

func (l *loader) assembleObject(convert *converter, object wireObject, at origin) {
	kind, known := parseClosed(objectKinds(), ObjectKind.envelopeKind, object.Kind)
	if !known {
		convert.keep(invalidValue("unknown object kind"))
		return
	}
	a := &l.assembly
	spec := at.child("/spec")
	switch kind {
	case ObjectWorkload:
		var wire wireWorkloadSpec
		convert.keep(decodeStrict(object.Spec, &wire))
		a.workloads = append(a.workloads, located[Workload]{value: convert.workload(object.Key, wire, object.Metadata), at: at, spec: spec})
	case ObjectModule:
		var wire wireModuleSpec
		convert.keep(decodeStrict(object.Spec, &wire))
		instance := convert.moduleInstance(object.Key, wire, object.Metadata, spec.child("/options"))
		a.modules = append(a.modules, located[ModuleInstance]{value: instance, at: at, spec: spec})
	case ObjectClient:
		var wire wireClientSpec
		convert.keep(decodeStrict(object.Spec, &wire))
		a.clients = append(a.clients, located[Client]{value: convert.client(object.Key, wire, object.Metadata), at: at, spec: spec})
	}
}

func (c *converter) provenance(wire wireProvenanceEntry) Provenance {
	provenance := Provenance{declared: c.origin(wire.Declared.Origin, wire.Declared.File, wire.Declared.Line, wire.Declared.Column)}
	for _, patch := range wire.Patched {
		provenance.patches = append(provenance.patches, Patch{
			origin: c.origin(patch.Origin, patch.File, patch.Line, patch.Column),
			fields: patch.Fields,
		})
	}
	return provenance
}

func (c *converter) origin(kind string, file *string, line, column *int) Origin {
	originKind, known := parseClosed(originKinds(), OriginKind.String, kind)
	if !known {
		c.keep(invalidValue("unknown origin"))
	}
	result := Origin{kind: originKind, file: optionalOf(file)}
	if line != nil {
		result.line = *line
	}
	if column != nil {
		result.column = *column
	}
	return result
}

// loadLocalDefinitions loads each project-local definition through the same contract a packaged
// module uses. A directory loads once however many entries name it, and its diagnostics are reported
// once, at the first entry that names it. Past maxDefinitionDirs directories it reports the limit at
// the entry that would load one more and stops.
func (l *loader) loadLocalDefinitions() Diagnostics {
	var diagnostics Diagnostics
	loaded := map[string]optional[ModuleDefinition]{}
	for _, entry := range l.assembly.definitions {
		dir := path.Join(l.dir, entry.value.path)
		result, seen := loaded[dir]
		if !seen {
			if len(loaded) == maxDefinitionDirs {
				return append(diagnostics, newDiagnostic(SyntaxLimitExceeded, entry.at.location("/path"),
					fmt.Sprintf("a project loads local definitions from at most %d directories", maxDefinitionDirs)))
			}
			definition, found := l.loadDefinition(dir, OriginLocal, some(entry.at.location("/path")))
			diagnostics = append(diagnostics, found...)
			if len(found) == 0 {
				result = some(definition)
			}
			loaded[dir] = result
		}
		definition, ok := result.get()
		if _, named := l.local[entry.value.name]; ok && !named {
			l.local[entry.value.name] = definition
		}
	}
	return diagnostics
}

// definition returns the definition an instance uses: a local one first, then a supplied package.
func (l *loader) definition(name ModuleName) (ModuleDefinition, bool) {
	if definition, ok := l.local[name]; ok {
		return definition, true
	}
	for _, definition := range l.packaged {
		if definition.name == name {
			return definition, true
		}
	}
	return ModuleDefinition{}, false
}

// checkStructure validates instance options against their definitions, including each
// environment's merged options, and refuses secret values written into the model.
func (l *loader) checkStructure() Diagnostics {
	var diagnostics Diagnostics
	l.failed = map[ObjectKey]bool{}
	l.overrideValues = maxOverrideValues
	l.checkUnits = maxOptionCheckUnits
	l.optionValues = map[ObjectKey]int{}
	secret := map[InputKey]bool{}
	for _, input := range l.assembly.inputs {
		if _, seen := secret[input.value.key]; !seen {
			secret[input.value.key] = input.value.sensitivity == SensitivitySecret
		}
		if _, hasValue := input.value.value.get(); hasValue && input.value.sensitivity == SensitivitySecret {
			diagnostics = append(diagnostics, secretLiteral(input.at.location("/value"), input.value.key))
		}
	}
	for _, instance := range l.assembly.modules {
		definition, ok := l.definition(instance.value.definition)
		if !ok {
			continue
		}
		tree := optionsTree(instance.value.optionsAt)
		if !l.spendCheckUnits(definition.optionCheckCost(tree)) {
			return append(diagnostics, newDiagnostic(SyntaxLimitExceeded, instance.value.optionsAt.location(""), optionCheckLimitMessage()))
		}
		found := optionDiagnostics(definition, tree, instance.value.optionsAt)
		if len(found) > 0 {
			l.failed[instance.value.key] = true
		}
		diagnostics = append(diagnostics, found...)
	}
	for _, environment := range l.assembly.environments {
		found, stopped := l.checkEnvironmentStructure(environment, secret)
		diagnostics = append(diagnostics, found...)
		if stopped {
			break
		}
	}
	return diagnostics
}

func secretLiteral(at Location, key InputKey) Diagnostic {
	return newDiagnostic(StructureSecretLiteral, at,
		fmt.Sprintf("input %q is secret, so the model may name only where its value comes from, never the value", key))
}

// checkEnvironmentStructure checks one environment's input values and merged module options. It
// reports stopped when the override checks would walk past maxOverrideValues, after reporting the
// limit at the override that would exceed it.
func (l *loader) checkEnvironmentStructure(environment located[Environment], secret map[InputKey]bool) (diagnostics Diagnostics, stopped bool) {
	for _, sources := range environment.value.inputSources {
		if _, hasValue := sources.value.get(); hasValue && secret[sources.Key] {
			diagnostics = append(diagnostics, secretLiteral(environment.at.location(joinPointer("/inputs", sources.Key.value)+"/value"), sources.Key))
		}
	}
	for _, override := range environment.value.moduleOverrides {
		instance, ok := l.module(override.Key)
		if !ok || l.failed[override.Key] {
			continue
		}
		definition, ok := l.definition(instance.definition)
		if !ok {
			continue
		}
		overrideAt := overrideOptionsAt(environment.at, override.Key)
		base, overrides := optionsTree(instance.optionsAt), optionsTree(overrideAt)
		if !l.spendOverrideValues(l.instanceOptionValues(override.Key, base) + countValues(overrides)) {
			return append(diagnostics, overrideLimitExceeded(overrideAt.location(""))), true
		}
		merged := mergeOptions(base, overrides)
		if !l.spendCheckUnits(definition.optionCheckCost(merged)) {
			return append(diagnostics, newDiagnostic(SyntaxLimitExceeded, overrideAt.location(""), optionCheckLimitMessage())), true
		}
		diagnostics = append(diagnostics, optionDiagnostics(definition, merged, overrideAt)...)
	}
	return diagnostics, false
}

// overrideOptionsAt is where the options of the override of module key sit, in the environment declared at at.
func overrideOptionsAt(at origin, key ObjectKey) origin {
	return at.child(joinPointer("/overrides/modules", key.value) + "/options")
}

// overrideOrigins records where each module override of environment sits, for EffectiveOptions.
func overrideOrigins(environment Environment, at origin) map[ObjectKey]origin {
	origins := make(map[ObjectKey]origin, len(environment.moduleOverrides))
	for _, override := range environment.moduleOverrides {
		origins[override.Key] = overrideOptionsAt(at, override.Key)
	}
	return origins
}

func overrideLimitExceeded(at Location) Diagnostic {
	return newDiagnostic(SyntaxLimitExceeded, at, fmt.Sprintf("the environment override checks of one project "+
		"walk at most %d option values; each override counts its instance's options and its own", maxOverrideValues))
}

// instanceOptionValues returns the value count of the options of instance key, counting them once.
func (l *loader) instanceOptionValues(key ObjectKey, options any) int {
	count, counted := l.optionValues[key]
	if !counted {
		count = countValues(options)
		l.optionValues[key] = count
	}
	return count
}

// spendOverrideValues takes values from the override budget, or reports false and takes nothing when
// fewer remain.
func (l *loader) spendOverrideValues(values int) bool {
	if values > l.overrideValues {
		return false
	}
	l.overrideValues -= values
	return true
}

// spendCheckUnits takes units from the evaluation budget, or reports false and takes nothing when
// fewer remain.
func (l *loader) spendCheckUnits(units int) bool {
	if units > l.checkUnits {
		return false
	}
	l.checkUnits -= units
	return true
}

func optionCheckLimitMessage() string {
	return fmt.Sprintf("the options checks of one project may evaluate at most %d units in all; "+
		"each check counts its option values times the option schema's size with references expanded", maxOptionCheckUnits)
}

func optionDiagnostics(definition ModuleDefinition, tree any, at origin) Diagnostics {
	return violationDiagnostics(definition.checkOptions(tree), at)
}

// optionsTree returns the options object declared at at, or an empty object when none is.
func optionsTree(at origin) any {
	if at.doc == nil {
		return map[string]any{}
	}
	tree, ok := at.doc.subtree(at.pointer)
	if !ok {
		return map[string]any{}
	}
	return tree
}

// mergeOptions applies an environment override: objects merge by key, scalars and lists replace.
func mergeOptions(base, override any) any {
	return overlayOptions(base, override, "", nil)
}

// build returns the Project from the first declaration of each name, sorted.
func (l *loader) build() Project {
	a := l.assembly
	project := Project{
		name:         a.name,
		description:  a.description,
		manifestPath: a.manifestPath,
		lockPath:     a.lockPath,
		manifest:     a.manifest,
		lock:         a.lock,
		unresolved:   l.unresolved,
		provenance:   a.provenance,
	}
	project.localDefinitions = firstOf(a.definitions, func(d localDefinition) string { return d.name.value })
	for _, name := range sortedKeys(l.resolvedByName()) {
		project.definitions = append(project.definitions, l.resolved[ModuleName{value: name}])
	}
	project.inputs = firstOf(a.inputs, func(i Input) string { return i.key.value })
	project.workloads = firstOf(a.workloads, func(w Workload) string { return w.key.value })
	project.modules = firstOf(a.modules, func(m ModuleInstance) string { return m.key.value })
	project.clients = firstOf(a.clients, func(c Client) string { return c.key.value })
	project.applications = firstOf(a.applications, func(app Application) string { return app.key.value })
	project.bindings = firstOf(a.bindings, func(b Binding) string { return b.ref.String() })
	project.environments = firstOf(a.environments, func(e Environment) string { return e.name.value })
	project.workloadsByKey = indexBy(project.workloads, func(w Workload) ObjectKey { return w.key })
	project.modulesByKey = indexBy(project.modules, func(m ModuleInstance) ObjectKey { return m.key })
	project.definitionsByName = indexBy(project.definitions, func(d ModuleDefinition) ModuleName { return d.name })
	return project
}

func (l *loader) resolvedByName() map[string]ModuleDefinition {
	byName := make(map[string]ModuleDefinition, len(l.resolved))
	for name, definition := range l.resolved {
		byName[name.value] = definition
	}
	return byName
}

// firstOf keeps the first declaration of each identity, ordered by identity.
func firstOf[T any](items []located[T], identity func(T) string) []T {
	seen := map[string]T{}
	for _, item := range items {
		id := identity(item.value)
		if _, exists := seen[id]; !exists {
			seen[id] = item.value
		}
	}
	values := make([]T, 0, len(seen))
	for _, id := range sortedKeys(seen) {
		values = append(values, seen[id])
	}
	return values
}
