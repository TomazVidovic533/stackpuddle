package model

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
)

const (
	descriptorFile = "module.yaml"
	// maxOptionSchemaValues caps an option schema's values, and so its subschemas, both as written
	// and with every "#/$defs/" reference expanded. Compiling and checking a schema costs more than
	// linear time in its subschemas, so this cap, not the file limits, keeps it small.
	maxOptionSchemaValues = 1 << 11
)

// LoadModuleDefinition reads the Module Definition whose module.yaml sits at the root of fsys, with
// its option schema and defaults. Pass an os.Root-backed file system for untrusted trees. The
// option schema compiles with all loading refused, so it can reference nothing outside itself. On
// any diagnostic the definition is the zero value.
func LoadModuleDefinition(fsys fs.FS) (ModuleDefinition, Diagnostics) {
	reader, diagnostics := newReader(fsys)
	if len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	definition, diagnostics := reader.loadDefinition(".", OriginPackaged, optional[Location]{})
	if len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics.Sorted()
	}
	return definition, nil
}

// loadDefinition reads the definition in dir, a directory of the reader's file system.
func (r *reader) loadDefinition(dir string, definitionOrigin DefinitionOrigin, referencedAt optional[Location]) (ModuleDefinition, Diagnostics) {
	doc, diagnostics := r.read(path.Join(dir, descriptorFile), referencedAt)
	if len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	if diagnostics := compatibility(doc); len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	if diagnostics := r.structure(doc, kindModule); len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	var wire wireModule
	if diagnostics := doc.decode(&wire); len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	if diagnostics := r.checkContentFiles(doc, dir, wire); len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	definition, diagnostics := r.definitionFrom(doc, dir, wire)
	if len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	definition.origin = definitionOrigin
	workloads := make([]located[Workload], 0, len(definition.workloads))
	for _, workload := range definition.workloads {
		at := origin{doc: doc, pointer: joinPointer("/workloads", workload.key.value)}
		workloads = append(workloads, located[Workload]{value: workload, at: at, spec: at})
	}
	checks := []func() Diagnostics{
		func() Diagnostics { return definitionIdentity(workloads) },
		func() Diagnostics { return r.definitionReferences(dir, workloads) },
		func() Diagnostics { return prerequisiteCycles(workloads) },
	}
	for _, check := range checks {
		if diagnostics := check(); len(diagnostics) > 0 {
			return ModuleDefinition{}, diagnostics
		}
	}
	return definition, nil
}

// contentFiles returns each content file the descriptor names and the pointer that names it.
func contentFiles(wire wireModule) map[string]string {
	files := map[string]string{}
	if wire.Configuration != nil {
		files["/configuration/schema"] = wire.Configuration.Schema
		if wire.Configuration.Defaults != nil {
			files["/configuration/defaults"] = *wire.Configuration.Defaults
		}
	}
	if wire.Runtimes != nil && wire.Runtimes.Compose != nil {
		files["/runtimes/compose/file"] = wire.Runtimes.Compose.File
		if wire.Runtimes.Compose.HostAccessFile != nil {
			files["/runtimes/compose/hostAccessFile"] = *wire.Runtimes.Compose.HostAccessFile
		}
	}
	if wire.Provisioning != nil {
		files["/provisioning/contract"] = wire.Provisioning.Contract
	}
	return files
}

func (r *reader) checkContentFiles(doc *document, dir string, wire wireModule) Diagnostics {
	var diagnostics Diagnostics
	files := contentFiles(wire)
	for _, pointer := range sortedKeys(files) {
		info, err := fs.Stat(r.fsys, path.Join(dir, files[pointer]))
		if err != nil || !info.Mode().IsRegular() {
			diagnostics = append(diagnostics, newDiagnostic(ReferencePathNotFound, doc.at(pointer),
				fmt.Sprintf("content file %q is not a regular file in the module", files[pointer])))
		}
	}
	return diagnostics
}

func (r *reader) definitionFrom(doc *document, dir string, wire wireModule) (ModuleDefinition, Diagnostics) {
	convert := converter{}
	definition := ModuleDefinition{
		name:       convert.moduleName(wire.Name),
		version:    convert.version(wire.Version),
		descriptor: doc,
	}
	if wire.DataFormat != nil {
		definition.dataFormat = some(convert.dataFormat(*wire.DataFormat))
	}
	if wire.Requires != nil {
		for _, capability := range wire.Requires.Capabilities {
			definition.requires = append(definition.requires, convert.capability(capability))
		}
		slices.SortFunc(definition.requires, func(a, b Capability) int { return compareText(a.value, b.value) })
	}
	for _, key := range sortedKeys(wire.Workloads) {
		declaration := wire.Workloads[key]
		definition.workloads = append(definition.workloads, convert.workload(key, declaration.wireWorkloadSpec, declaration.Metadata))
	}
	if diagnostics := convert.diagnostics(doc, ""); len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	if wire.Configuration == nil {
		return definition, nil
	}
	options, optionCost, diagnostics := r.compileOptionSchema(doc, path.Join(dir, wire.Configuration.Schema))
	if len(diagnostics) > 0 {
		return ModuleDefinition{}, diagnostics
	}
	definition.options = options
	definition.optionCost = optionCost
	if wire.Configuration.Defaults != nil {
		defaults, defaultsDoc, diagnostics := r.readDefaults(path.Join(dir, *wire.Configuration.Defaults))
		if len(diagnostics) > 0 {
			return ModuleDefinition{}, diagnostics
		}
		definition.defaults = some(defaults)
		definition.defaultsAt = origin{doc: defaultsDoc}
	}
	return definition, nil
}

// compileOptionSchema compiles the module's option schema with every reference load refused: an
// option schema may only refer to itself, never to local files or the network, and only through
// "#/$defs/<name>" references that form no cycle. It returns the schema's size with those references
// expanded, which each options check is charged per option value (see maxOptionCheckUnits).
func (r *reader) compileOptionSchema(descriptor *document, file string) (*schemacheck.Set, int, Diagnostics) {
	schemaDoc, diagnostics := r.read(file, some(descriptor.at("/configuration/schema")))
	if len(diagnostics) > 0 {
		return nil, 0, diagnostics
	}
	limitExceeded := func() Diagnostics {
		return Diagnostics{newDiagnostic(SyntaxLimitExceeded, schemaDoc.at(""),
			fmt.Sprintf("an option schema holds at most %d values, with its references expanded", maxOptionSchemaValues))}
	}
	if countValues(schemaDoc.tree) > maxOptionSchemaValues {
		return nil, 0, limitExceeded()
	}
	cost, err := schemacheck.ExpandedValues(schemaDoc.tree, maxOptionSchemaValues)
	switch {
	case errors.Is(err, schemacheck.ErrTooLarge):
		return nil, 0, limitExceeded()
	case err != nil:
		return nil, 0, Diagnostics{newDiagnostic(StructureInvalidSchema, descriptor.at("/configuration/schema"),
			"an option schema may only use \"#/$defs/<name>\" references that form no cycle, and no $dynamicRef or $anchor")}
	}
	set, err := schemacheck.CompileDocument(optionSchemaID, schemaDoc.tree)
	if err != nil {
		return nil, 0, Diagnostics{newDiagnostic(StructureInvalidSchema, descriptor.at("/configuration/schema"),
			"the option schema does not compile with references to other files and the network refused")}
	}
	return set, cost, nil
}

// readDefaults parses the defaults file. Defaults are not validated alone: the effective options,
// defaults merged with every layer, are what ValidateOptions checks.
func (r *reader) readDefaults(file string) ([]byte, *document, Diagnostics) {
	doc, diagnostics := r.read(file, optional[Location]{})
	if len(diagnostics) > 0 {
		return nil, nil, diagnostics
	}
	if _, isObject := doc.tree.(map[string]any); !isObject {
		return nil, nil, Diagnostics{newDiagnostic(StructureInvalidType, doc.at(""), "module defaults must be a mapping")}
	}
	value, err := canonical.FromTree(doc.tree)
	if err != nil {
		return nil, nil, Diagnostics{newDiagnostic(StructureInvalidValue, doc.at(""), "module defaults are not JSON-compatible")}
	}
	return canonical.Encode(value), doc, nil
}

// definitionIdentity keeps Export and Dependency keys unique across a definition's workloads, so an
// instance's Export or Dependency is named by its key alone.
func definitionIdentity(workloads []located[Workload]) Diagnostics {
	var diagnostics Diagnostics
	exports := map[ExportKey]origin{}
	dependencies := map[DependencyKey]origin{}
	for _, workload := range workloads {
		for _, export := range workload.value.exports {
			at := workload.spec.child(joinPointer("/exports", export.Key.value))
			if earlier, seen := exports[export.Key]; seen {
				diagnostics = append(diagnostics, newDiagnostic(IdentityDuplicateDeclaration, at.location(""),
					fmt.Sprintf("export %q is declared by more than one workload of the definition", export.Key), earlier.location("")))
				continue
			}
			exports[export.Key] = at
		}
		for _, dependency := range workload.value.dependencies {
			at := workload.spec.child(joinPointer("/dependencies", dependency.Key.value))
			if earlier, seen := dependencies[dependency.Key]; seen {
				diagnostics = append(diagnostics, newDiagnostic(IdentityDuplicateDeclaration, at.location(""),
					fmt.Sprintf("dependency %q is declared by more than one workload of the definition", dependency.Key), earlier.location("")))
				continue
			}
			dependencies[dependency.Key] = at
		}
	}
	return diagnostics
}

// definitionReferences checks a definition's workloads. A definition declares no inputs, so its
// workloads cannot take a value from one.
func (r *reader) definitionReferences(dir string, workloads []located[Workload]) Diagnostics {
	scope := workloadScope{
		workload: func(key ObjectKey) (Workload, bool) {
			for _, workload := range workloads {
				if workload.value.key == key {
					return workload.value, true
				}
			}
			return Workload{}, false
		},
		input:    func(InputKey) bool { return false },
		fsys:     r.fsys,
		buildDir: dir,
	}
	var diagnostics Diagnostics
	for _, workload := range workloads {
		diagnostics = append(diagnostics, scope.check(workload)...)
	}
	return diagnostics
}
