package model

import (
	"slices"
	"strconv"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

// Encode returns the canonical Model document of p: UTF-8 JSON with keys in UTF-16 order, two-space
// indentation and a trailing newline, the bytes JSON.stringify(model, null, 2) prints. Optional
// members that are absent, empty or false are omitted; options and parameters keep their values and
// change only in key order and layout. The same project always encodes to the same bytes, and
// loading the bytes as a Model root encodes to them again. The zero Project encodes to an invalid
// Model.
func Encode(p Project) []byte {
	return canonical.Encode(encodeModel(p))
}

func encodeModel(p Project) canonical.Object {
	model := canonical.Object{
		"apiVersion": canonical.String(APIVersion),
		"kind":       canonical.String(kindModel),
		"metadata":   encodeProjectMetadata(p.name, p.description),
		"spec":       encodeSpec(p),
	}
	if len(p.provenance) > 0 {
		model["provenance"] = encodeProvenance(p.provenance)
	}
	return model
}

func encodeProjectMetadata(name ProjectName, description optional[string]) canonical.Object {
	metadata := canonical.Object{"name": canonical.String(name.value)}
	if text, ok := description.get(); ok {
		metadata["description"] = canonical.String(text)
	}
	return metadata
}

// putMetadata adds a "metadata" member only when the declaration has a description.
func putMetadata(object canonical.Object, description optional[string]) {
	if text, ok := description.get(); ok {
		object["metadata"] = canonical.Object{"description": canonical.String(text)}
	}
}

func putString(object canonical.Object, key string, value optional[string]) {
	if text, ok := value.get(); ok {
		object[key] = canonical.String(text)
	}
}

// putNonEmpty adds a member only when it holds something; empty optional members are omitted.
func putNonEmpty[V canonical.Object | canonical.Array](object canonical.Object, key string, value V) {
	if len(value) > 0 {
		object[key] = canonical.Value(value)
	}
}

func encodeSpec(p Project) canonical.Object {
	spec := canonical.Object{}
	putString(spec, "manifest", p.manifestPath)
	putString(spec, "lock", p.lockPath)
	definitions := canonical.Object{}
	for _, definition := range p.localDefinitions {
		definitions[definition.name.value] = canonical.Object{"path": canonical.String(definition.path)}
	}
	putNonEmpty(spec, "definitions", definitions)
	inputs := canonical.Object{}
	for _, input := range p.inputs {
		object := canonical.Object{"sensitivity": canonical.String(input.sensitivity.String())}
		putString(object, "value", input.value)
		inputs[input.key.value] = object
	}
	putNonEmpty(spec, "inputs", inputs)
	putNonEmpty(spec, "objects", encodeObjects(p))
	applications := canonical.Object{}
	for _, application := range p.applications {
		object := canonical.Object{"workloads": encodeKeys(application.workloads)}
		if application.isDefault {
			object["default"] = canonical.Bool(true)
		}
		putMetadata(object, application.description)
		applications[application.key.value] = object
	}
	putNonEmpty(spec, "applications", applications)
	bindings := canonical.Array{}
	for _, binding := range p.bindings {
		bindings = append(bindings, encodeBinding(binding))
	}
	putNonEmpty(spec, "bindings", bindings)
	environments := canonical.Object{}
	for _, environment := range p.environments {
		environments[environment.name.value] = encodeEnvironment(environment)
	}
	putNonEmpty(spec, "environments", environments)
	return spec
}

// encodeObjects returns the object envelopes ordered by kind name, then key.
func encodeObjects(p Project) canonical.Array {
	type envelope struct {
		kind, key string
		value     canonical.Object
	}
	var envelopes []envelope
	add := func(kind ObjectKind, key ObjectKey, description optional[string], spec canonical.Object) {
		object := canonical.Object{
			"apiVersion": canonical.String(APIVersion),
			"kind":       canonical.String(kind.envelopeKind()),
			"key":        canonical.String(key.value),
			"spec":       spec,
		}
		putMetadata(object, description)
		envelopes = append(envelopes, envelope{kind: kind.envelopeKind(), key: key.value, value: object})
	}
	for _, workload := range p.workloads {
		add(ObjectWorkload, workload.key, workload.description, encodeWorkloadSpec(workload))
	}
	for _, instance := range p.modules {
		spec := canonical.Object{"definition": canonical.String(instance.definition.value)}
		putOptions(spec, "options", instance.options)
		add(ObjectModule, instance.key, instance.description, spec)
	}
	for _, client := range p.clients {
		add(ObjectClient, client.key, client.description, canonical.Object{"dependencies": encodeDependencies(client.dependencies)})
	}
	slices.SortFunc(envelopes, func(a, b envelope) int {
		if c := compareText(a.kind, b.kind); c != 0 {
			return c
		}
		return compareText(a.key, b.key)
	})
	objects := make(canonical.Array, 0, len(envelopes))
	for _, item := range envelopes {
		objects = append(objects, item.value)
	}
	return objects
}

// putOptions adds module-defined JSON, kept as written, unless it is an empty object.
func putOptions(object canonical.Object, key string, data []byte) {
	if len(data) == 0 || isEmptyObject(data) {
		return
	}
	value, err := canonical.Parse(data)
	if err != nil {
		// Options are stored only after canonical.Parse accepted them, so this cannot happen.
		return
	}
	object[key] = value
}

func encodeWorkloadSpec(workload Workload) canonical.Object {
	spec := canonical.Object{"type": canonical.String(workload.workloadType.String())}
	switch artifact := workload.artifact.(type) {
	case ImageArtifact:
		spec["image"] = canonical.String(artifact.reference)
	case BuildArtifact:
		build := canonical.Object{"context": canonical.String(artifact.context)}
		putString(build, "dockerfile", artifact.dockerfile)
		putString(build, "target", artifact.target)
		spec["build"] = build
	}
	command := canonical.Array{}
	for _, argument := range workload.command {
		command = append(command, canonical.String(argument))
	}
	putNonEmpty(spec, "command", command)
	putNonEmpty(spec, "environment", encodeEnvironmentMap(workload.environment))
	listeners := canonical.Object{}
	for _, listener := range workload.listeners {
		listeners[listener.Key.value] = canonical.Object{
			"port": canonical.Number(strconv.Itoa(listener.Port)), "protocol": canonical.String(listener.Protocol),
		}
	}
	putNonEmpty(spec, "listeners", listeners)
	exports := canonical.Object{}
	for _, export := range workload.exports {
		object := canonical.Object{"capability": canonical.String(export.Capability.value), "listener": canonical.String(export.Listener.value)}
		values := canonical.Object{}
		for _, value := range export.values {
			values[value.Name.value] = canonical.Object{"sensitivity": canonical.String(value.Sensitivity.String())}
		}
		putNonEmpty(object, "values", values)
		exports[export.Key.value] = object
	}
	putNonEmpty(spec, "exports", exports)
	putNonEmpty(spec, "dependencies", encodeDependencies(workload.dependencies))
	volumes := canonical.Object{}
	for _, volume := range workload.volumes {
		volumes[volume.Key.value] = canonical.Object{"path": canonical.String(volume.Path)}
	}
	putNonEmpty(spec, "volumes", volumes)
	prerequisites := canonical.Array{}
	for _, job := range workload.Prerequisites() {
		prerequisites = append(prerequisites, canonical.Object{"job": canonical.String(job.value)})
	}
	putNonEmpty(spec, "prerequisites", prerequisites)
	return spec
}

func encodeDependencies(dependencies []Dependency) canonical.Object {
	object := canonical.Object{}
	for _, dependency := range dependencies {
		object[dependency.Key.value] = canonical.Object{"capability": canonical.String(dependency.Capability.value)}
	}
	return object
}

func encodeEnvironmentMap(variables []EnvVar) canonical.Object {
	object := canonical.Object{}
	for _, variable := range variables {
		switch value := variable.Value.(type) {
		case LiteralValue:
			object[variable.Key.value] = canonical.String(value.text)
		case InputValue:
			object[variable.Key.value] = canonical.Object{"input": canonical.String(value.input.value)}
		}
	}
	return object
}

func encodeKeys(keys []ObjectKey) canonical.Array {
	array := canonical.Array{}
	for _, key := range sortedObjectKeys(keys) {
		array = append(array, canonical.String(key.value))
	}
	return array
}

func encodeExportRef(ref ExportRef) canonical.Object {
	return canonical.Object{"object": canonical.String(ref.Object.String()), "export": canonical.String(ref.Export.value)}
}

func encodeBinding(binding Binding) canonical.Object {
	object := canonical.Object{
		"consumer":   canonical.String(binding.ref.Consumer.String()),
		"dependency": canonical.String(binding.ref.Dependency.value),
		"source":     encodeExportRef(binding.source),
	}
	if parameters, ok := binding.parameters.get(); ok {
		putOptions(object, "parameters", parameters)
	}
	if delivery, ok := binding.delivery.get(); ok {
		encoded := canonical.Object{}
		environment := canonical.Object{}
		for _, variable := range delivery.environment {
			environment[variable.Key.value] = canonical.String(variable.Value.value)
		}
		putNonEmpty(encoded, "environment", environment)
		files := canonical.Object{}
		for _, file := range delivery.files {
			entry := canonical.Object{"path": canonical.String(file.Path), "mode": canonical.String(file.Mode.String())}
			putString(entry, "owner", file.owner)
			files[file.Value.value] = entry
		}
		putNonEmpty(encoded, "files", files)
		object["delivery"] = encoded
	}
	return object
}

func encodeEnvironment(environment Environment) canonical.Object {
	object := canonical.Object{
		"secrets": canonical.Object{"provider": canonical.String(environment.secretProvider.value)},
	}
	if compose, ok := environment.runtime.(ComposeRuntime); ok {
		object["runtime"] = canonical.Object{"type": canonical.String(composeRuntimeType), "context": canonical.String(compose.context)}
	}
	exposures := canonical.Array{}
	for _, exposure := range environment.Exposures() {
		entry := canonical.Object{"source": encodeExportRef(exposure.Source)}
		if port, ok := exposure.hostPort.get(); ok {
			entry["port"] = canonical.Number(strconv.Itoa(port))
		}
		exposures = append(exposures, entry)
	}
	putNonEmpty(object, "exposures", exposures)
	inputs := canonical.Object{}
	for _, sources := range environment.inputSources {
		entry := canonical.Object{}
		putString(entry, "value", sources.value)
		if local, ok := sources.local.get(); ok {
			entry["local"] = canonical.Object{"file": canonical.String(local.File), "key": canonical.String(local.Key.value)}
		}
		if process, ok := sources.process.get(); ok {
			entry["process"] = canonical.String(process.value)
		}
		inputs[sources.Key.value] = entry
	}
	putNonEmpty(object, "inputs", inputs)
	overrides := canonical.Object{}
	workloads := canonical.Object{}
	for _, override := range environment.workloadOverrides {
		workloads[override.Key.value] = canonical.Object{"environment": encodeEnvironmentMap(override.Environment)}
	}
	putNonEmpty(overrides, "workloads", workloads)
	modules := canonical.Object{}
	for _, override := range environment.moduleOverrides {
		entry := canonical.Object{"options": canonical.Object{}}
		putOptions(entry, "options", override.Options)
		modules[override.Key.value] = entry
	}
	putNonEmpty(overrides, "modules", modules)
	putNonEmpty(object, "overrides", overrides)
	putMetadata(object, environment.description)
	return object
}

func encodeProvenance(provenance map[Ref]Provenance) canonical.Object {
	object := canonical.Object{}
	for ref, entry := range provenance {
		encoded := canonical.Object{"declared": encodeOrigin(entry.declared)}
		patches := canonical.Array{}
		for _, patch := range entry.patches {
			encodedPatch := encodeOrigin(patch.origin)
			fields := canonical.Array{}
			for _, field := range patch.fields {
				fields = append(fields, canonical.String(field))
			}
			encodedPatch["fields"] = fields
			patches = append(patches, encodedPatch)
		}
		putNonEmpty(encoded, "patched", patches)
		object[ref.text] = encoded
	}
	return object
}

func encodeOrigin(origin Origin) canonical.Object {
	object := canonical.Object{"origin": canonical.String(origin.kind.String())}
	putString(object, "file", origin.file)
	if origin.line > 0 {
		object["line"] = canonical.Number(strconv.Itoa(origin.line))
	}
	if origin.column > 0 {
		object["column"] = canonical.Number(strconv.Itoa(origin.column))
	}
	return object
}
