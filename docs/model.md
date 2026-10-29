# The StackPuddle project model

A project is declared once, in YAML or with an authoring SDK, and every tool reads the same
versioned model from it. The [`model`](../model) package parses `stackpuddle/v1` files, checks them
before anything runs, and writes the canonical Model document that the SDKs also write. Loading a
project reads only the files it is given and provisions nothing.

## Documents

Every document starts with `apiVersion: stackpuddle/v1` and a `kind`. The schema is the contract for
each document's fields; this page describes behaviour.

| Kind | Schema | Purpose |
| --- | --- | --- |
| Project | [project](../schemas/v1/project.schema.json) | A project root: its declarations and the Fragment files it imports |
| Fragment | [fragment](../schemas/v1/fragment.schema.json) | Declarations a Project imports |
| Model | [model](../schemas/v1/model.schema.json) | The canonical, resolved form of a project, readable as a root |
| Module | [module](../schemas/v1/module.schema.json) | A Module Definition, packaged or project-local |
| Bundle | [bundle](../schemas/v1/bundle.schema.json) | A tested set: one version per module |
| Manifest | [manifest](../schemas/v1/manifest.schema.json) | The bundle version and packaged modules a project selects |
| Lock | [lock](../schemas/v1/lock.schema.json) | The exact archives a Manifest resolved to, by digest |
| Bindings | [bindings](../schemas/v1/bindings.schema.json) | The endpoint each consumer reaches in one environment |
| VersionInfo | [version-info](../schemas/v1/version-info.schema.json) | What a build is and which contract versions it reads |

Shared rules, such as name patterns and the workload shape, live in the
[definitions schema](../schemas/v1/definitions.schema.json). The Model schema holds configuration
intent and secret references only, never secret values.

## The composition root

A project's root file is `stackpuddle.yaml` by default. The root may list Fragment files under
`spec.imports`; a Fragment imports nothing, so imports never form a cycle. Files next to the root are
not merged unless the root imports them. Relative paths in any file resolve against the root file's
directory. A name declared in two files is an error, never a merge.

The root may instead be a canonical Model file. It loads as written, provenance included.

## Objects and references

A project declares three kinds of object, each under a key of 1 to 30 lowercase letters, digits and
single hyphens:

- a workload, a container that runs as a `service` or a `job`, from an `image` or a `build`;
- a module instance, one use of a Module Definition with its `options`;
- a client, a process on the host that StackPuddle does not run, such as a terminal or an IDE.

An application groups workloads; groups may overlap. Project and environment names follow the same
pattern with up to 32 characters and never contain `.`.

A workload offers Exports on its listeners and needs Dependencies. A module instance offers and
needs those of its definition's contributed workloads. A binding connects one Dependency of a
consumer to one Export. Any object can consume: a workload, a job, a module instance or a client.
The Dependency's capability must equal the Export's capability.

A binding has no name. Its identity is the consumer and the Dependency key, so changing its source
keeps its identity and its place in the canonical model. Command-line references use one grammar:

| Reference | Names |
| --- | --- |
| `workload/<key>`, `module/<key>`, `client/<key>` | a declared object |
| `app/<key>` | an application |
| `env/<name>` | an environment |
| `resource/<instance>/<type>/<name>` | a module resource; the module defines what it means |
| `operation/op-<26 Crockford base32 characters>` | an operation |
| `binding/<consumer kind>/<consumer key>/<dependency key>` | a binding, such as `binding/workload/api/database` |

## Environments

An environment selects its runtime and a named Docker context (`runtime: {type: compose, context:
desktop-linux}`), so an ambient context never replaces it. It names a secret provider; the set of
providers is open, and an engine checks the name against what it supports. It may expose Exports on
the host loopback interface, supply input values, and override workload environment variables and
module options for itself.

Inputs are declared once with a sensitivity. A public input may carry a project-level value. A
secret input never carries a value in the model; environments name where it comes from: a literal
(public inputs only), a key in an ignored local file, or an explicitly mapped process variable.
Applying input sources in order is the configuration layer's work, not the model's.

An instance's options have three layers, lowest first: the definition's defaults file, the options
the instance declares, and the selected environment's override of the instance. Objects merge by
key; scalars and lists replace. `Project.EffectiveOptions` returns the first two layers merged and
`Project.EnvironmentOptions` adds the third, each as canonical JSON. `EffectiveOptions.Source` names
the layer, file, pointer and position of any value, so a caller can explain a value or place a
finding in the file that wrote it; a pointer with no value of its own resolves to its nearest
ancestor. Both accessors return values, not verdicts: a caller checks them with
`ModuleDefinition.ValidateOptions`, and both report `ok` false for an unknown instance or
environment and for a definition the caller did not supply. Both the declared options and each
environment's merged options are checked against the definition's option schema at load.

A consumer's context follows from its kind: workloads and module instances run in a `container`,
clients on the `host`. The runtime adapter supplies the address of each Export for each context, and
resolving bindings never invents one. One Export can therefore resolve to `api:8080` for a container
consumer and to `127.0.0.1:49153` for a host client. A host client can reach only an Export the
environment exposes.

## Stages and diagnostics

Loading runs fixed stages over every file. A stage runs only when the stages before it found nothing,
so each diagnostic names a root cause, not its consequences:

1. syntax: the file is one UTF-8 YAML document in the JSON-compatible subset;
2. compatibility: `apiVersion` and `kind` are read first, and nothing else is checked when either is
   unknown;
3. structure: each document is the expected kind and valid against its schema, instance options are
   valid against their definition, and no secret value is written;
4. identity: each name is declared once;
5. reference: every path, object, Dependency, Export, listener, input and value name resolves;
6. binding and graph: bindings are unique, capabilities match, deliveries do not collide, every
   Dependency is bound and job prerequisites form no cycle.

Module semantics come after these stages and are not part of `Load`. A module's own rules, such as
the references between PostgreSQL users, roles and databases, live in the Go package `validation`,
which checks each instance's effective options (see Environments) and reports its own `<module>.<rule>`
codes. The schema proves shape and never claims to prove relationships; see
[PostgreSQL semantic rules](modules/postgres.md#semantic-rules).

A file another file names is read when its stage first needs it, so a missing import is reported
with the syntax stage and a missing local definition directory with the structure stage, both as
`reference.path_not_found`.

A diagnostic has a stable code, a file, a JSON pointer and, when known, a line and column. Messages
name pointers, identifiers and the expected constraint; they never repeat a declared value. Codes are
the contract; messages are not.

| Code | Meaning |
| --- | --- |
| `syntax.invalid_yaml` | The file is not valid YAML; the line is given when the parser reports one |
| `syntax.multiple_documents` | The file holds more than one YAML document |
| `syntax.duplicate_key` | A key appears twice in one mapping |
| `syntax.non_string_key` | A mapping key is not a string |
| `syntax.alias` | A YAML alias is used |
| `syntax.merge_key` | A `<<` merge key is used |
| `syntax.custom_tag` | A tag other than the core `str`, `bool`, `int`, `float` and `timestamp` is used |
| `syntax.non_json_number` | A number outside JSON number syntax or the double-precision range |
| `syntax.null_value` | A null or empty value; omit the member instead |
| `syntax.invalid_encoding` | The bytes are not UTF-8 |
| `syntax.limit_exceeded` | The file exceeds a parser limit (size, nesting, key length or value count), the project exceeds a load limit (definition directories, option-schema values or override checks), or a report was cut at the problem limit |
| `compatibility.unsupported_api_version` | The `apiVersion` is not one this reader reads; the message names those it does |
| `compatibility.unsupported_kind` | The `kind` is not a v1 kind; the message names the v1 kinds |
| `compatibility.module_not_in_bundle` | A Lock pins a module version outside its bundle's tested set |
| `compatibility.bundle_version_mismatch` | A Lock pins another bundle version than the bundle's |
| `structure.unknown_field` | A field the schema does not define |
| `structure.missing_field` | A required field is absent |
| `structure.invalid_type` | A value of the wrong JSON type |
| `structure.invalid_value` | A value outside the schema's constraint, such as a pattern or range |
| `structure.exactly_one` | Not exactly one of the schema's alternatives holds, such as image or build |
| `structure.unexpected_kind` | A document of another kind than its place requires |
| `structure.secret_literal` | A secret input's value is written into the project |
| `structure.invalid_schema` | A module's option schema does not compile with external references refused |
| `identity.duplicate_declaration` | A name, or an exposure of one Export, is declared twice |
| `identity.definition_name_mismatch` | A local definition's `module.yaml` names another module than its key |
| `reference.path_not_found` | A referenced file or directory does not exist |
| `reference.unknown_object` | A reference names an object, or bindings are resolved for an environment, that is not declared |
| `reference.wrong_kind` | A prerequisite names a service instead of a job |
| `reference.unknown_dependency` | A binding names a Dependency its consumer does not declare |
| `reference.unknown_export` | A reference names an Export its source does not offer |
| `reference.unknown_listener` | An Export names a listener its workload does not declare |
| `reference.unknown_definition` | An instance's definition is neither local nor supplied, with no Manifest; or a binding is resolved through an unresolved definition |
| `reference.unknown_input` | An environment variable or source names an undeclared input |
| `reference.unknown_value` | A delivery names a value the Export does not offer |
| `reference.module_not_in_manifest` | A packaged definition is used but the Manifest does not list it |
| `reference.invalid_ref` | A command-line reference does not follow the grammar |
| `binding.duplicate` | The same binding is declared twice |
| `binding.conflict` | One Dependency is bound twice with different sources |
| `binding.capability_mismatch` | The source's Export offers another capability than the Dependency needs |
| `binding.unbound_dependency` | A Dependency has no binding |
| `binding.delivery_conflict` | Two configuration paths set one environment variable or file |
| `binding.delivery_unsupported` | A delivery targets a client, which StackPuddle does not run |
| `binding.unreachable_context` | A host client's source is not exposed in the environment |
| `binding.address_unavailable` | The runtime supplies no address for a source in a consumer's context |
| `graph.prerequisite_cycle` | Jobs that wait for each other; reported once, at the smallest key |

A module's own validator reports codes of the form `<module>.<rule>`. When a Manifest lists a
packaged module that the caller did not supply, the project still loads, the module is reported as
unresolved, and every check that needs its definition is skipped rather than passed.

## The canonical model

The canonical Model is UTF-8 JSON that `JSON.stringify(model, null, 2)` prints after sorting object
keys, followed by a newline. Another language reproduces it byte for byte with these rules:

- Object keys sort by UTF-16 code units, the order JavaScript sorts strings in.
- Indentation is two spaces, the separator `": "`, one array element per line, and empty objects
  and arrays print as `{}` and `[]`. No `null` appears.
- Strings escape `"`, `\` and U+0000 to U+001F only: `\b \t \n \f \r` in short form, the others as
  `\u00xx` in lowercase hexadecimal. Nothing else is escaped.
- Integers within ±(2^53 − 1) print as integers; other numbers print as ECMAScript's
  Number-to-String prints them, so `1.0` prints as `1` and `1e-7` as `1e-7`.
- Optional members that are absent, empty or false are omitted. Inside `options` and `parameters`,
  which modules define, values stay as written and only key order and layout change.
- `spec.objects` holds one envelope per workload, module instance and client, ordered by kind name
  (`Client`, `ModuleInstance`, `Workload`) and then key. `spec.bindings` is ordered by binding
  reference and each environment's `exposures` by object and Export. Application members and
  prerequisites are sorted; a workload's `command` keeps its order. `imports` is not part of the
  model.
- `provenance` maps each declaration's reference (`workload/api`, `app/storefront`, `env/local`,
  `binding/workload/web/api`) to where it was declared: origin `yaml` with the file relative to the
  project directory and the line and column of its key, or origin `sdk`. An SDK's typed patches
  add `patched` entries naming the fields they set. Comparing two models' intent ignores
  `provenance`.

Loading a canonical Model and encoding it again gives the same bytes.

## Limits and compatibility

Each file is at most 1 MiB, nests at most 64 levels, holds at most 65,536 values (every mapping,
sequence and scalar that is not a key) and has no mapping key longer than 1024 bytes. YAML itself
limits implicit keys to 1024 characters; this limit covers explicit `? key` keys too. A file that
breaks one of these is refused with `syntax.limit_exceeded`. Parsing a file, or checking a document
or one instance's options against a schema, reports at most 100 problems; when it finds more, one
more `syntax.limit_exceeded` says the report was cut. A file's memory does not grow with key length
times nesting depth.

One Load reads the root file, at most 32 Fragment files it imports, its Manifest and its Lock, and
from each directory that local definitions name a `module.yaml`, option schema and defaults file. A
project names at most 16 such directories. A directory that several definitions name is read once,
and its problems are reported once, at the first definition that names it. Other paths, such as a
build context or a module's Compose file, are only checked to exist. An option schema may use only
`#/$defs/<name>` references to its own top-level `$defs`, no reference cycle, and no `$dynamicRef`
or `$anchor`; it holds at most 2,048 values as written and at most 2,048 with every reference
expanded (counted once per use), so a chain of references that doubles at each level is refused
at load with `syntax.limit_exceeded`. Checking is bounded by a per-Load budget of 8,388,608
(2^23) units: each options check, for an instance or an environment override, is charged the
instance's option value count times the schema's expanded size, and the check that would pass the
budget is refused with `syntax.limit_exceeded` at that instance or override
([ADR-0009](architecture/decisions/0009-option-schema-bounds.md)). `ModuleDefinition.ValidateOptions`
applies the same budget to one call.

An environment override of a module's options is checked as the instance's options merged with the
override, so each check walks both. The override checks of one project walk at most 262,144 (`maxOverrideValues`, a named, fixed default)
option values in all; the override that would pass that is reported with `syntax.limit_exceeded` and
checking stops there. Two growth tests in
[`tests/contracts/model/allocation`](../tests/contracts/model/allocation) bound load time: four
times the workloads, with applications naming each of them, load in under eight times as long, and
so do four times a module instance's options with four times the environments overriding them.

Within one file the schemas bound these collections: 1024 bindings, 64 exposures per environment,
256 workloads per application, 128 environment variables per workload, 64 command arguments, 32
delivered environment variables per binding, and 16 each of a workload's listeners, Exports,
Dependencies, volumes and prerequisites, of an Export's values, of a client's Dependencies, of a
binding's delivered files and of a definition's workloads. The workload, module, client,
application, environment, input and definition sections, an environment's input sources and
overrides, a Model's objects and provenance, and the modules of a Manifest, a Lock and a Bundle have
no count of their own; the value limit bounds them.

Names are short and closed: object keys up to 30 characters, project and environment names up to
32, module names up to 63. Loosening a limit is an additive change; tightening one is not.

Compatibility is declared by contract. A module declares the capabilities it requires and the data
format it keeps, a bundle lists the one tested version of each module, and VersionInfo publishes
the API versions a build reads. See [ADR-0006](architecture/decisions/0006-schema-identifiers.md) for
schema identifiers and [ADR-0007](architecture/decisions/0007-compatibility-representation.md) for
compatibility.

## Examples

The examples are executable: each is a conformance case whose expected model and diagnostics the
test suite checks.

- [The smallest project](../tests/conformance/cases/valid/minimal-service)
- [Builds, volumes and job prerequisites](../tests/conformance/cases/valid/job-build-volumes-prerequisites)
- [A PostgreSQL instance with a Manifest and a Lock](../tests/conformance/cases/valid/postgres-instance)
- [A project-local Module Definition](../tests/conformance/cases/valid/custom-definition)
- [Every kind of consumer, resolved for container and host](../tests/conformance/cases/valid/bindings-every-consumer)
- [One file](../tests/conformance/cases/valid/composition-single-file) and
  [the same project split across imports](../tests/conformance/cases/valid/composition-imports)
- [Inputs and per-environment overrides](../tests/conformance/cases/valid/inputs-and-overrides)
- [Overlapping applications](../tests/conformance/cases/valid/applications-overlap)
- [A Model file as the root](../tests/conformance/cases/valid/model-root)
- [A Manifest module nobody supplied](../tests/conformance/cases/valid/unresolved-packaged)

The [conformance suite](../tests/conformance/README.md) describes the case format.
