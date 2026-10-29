# Conformance cases

Reviewed cases of the `stackpuddle/v1` project model, shared by every reader and authoring SDK. Each
case pairs input files with the output they must produce: a canonical Model, resolved Bindings or
diagnostics. The rules the outputs follow are in [the model contract](../../docs/model.md).

Expected files are written by hand from the inputs and those rules, and reviewed apart from any
encoder. No runner regenerates them: a runner that disagrees with an expected file is the thing to
investigate.

Run the Go runner from the repository root:

```bash
GOWORK=off go test -race -count=1 ./tests/conformance/...
```

## Layout

```text
cases/
  valid/<case>/      inputs that load, with the outputs they produce
  invalid/<case>/    inputs that fail, each for one stated root cause
  parity/<case>/     the YAML halves of the cases an SDK declares in code
  semantics/<case>/  projects that load and whose module rules are checked, each for one stated rule
```

A case directory holds:

| File | Content |
| --- | --- |
| `case.json` | What the case is and how to run it (below) |
| inputs | YAML and JSON files, project-local definitions, and `packaged/<dir>/` module directories |
| `expected/model.json` | The canonical Model of a valid project case |
| `expected/diagnostics.json` | The diagnostics of an invalid case |
| `expected/bindings.json` | The canonical Bindings document, for a case with `addresses` |
| `expected/resolution-diagnostics.json` | The diagnostics of resolving bindings, instead of `bindings.json` |
| `expected/unresolved.json` | The names of Manifest modules nobody supplied, such as `["postgres"]` |

## case.json

| Member | Meaning |
| --- | --- |
| `description` | Required. What the case shows, in one or two sentences |
| `subject` | Required. `project`, `module`, `bundle-lock`, `version-info`, `versions` or `semantics` |
| `root` | The input to load: a project root file; a module directory; a Lock; a VersionInfo document; a JSON array of versions |
| `bundle` | `bundle-lock` only: the Bundle file the Lock is checked against |
| `packaged` | Packaged Module Definitions to supply: `{"repository": "<name>"}` for `modules/<name>` of this repository, `{"case": "packaged/<dir>"}` for a directory in the case |
| `environment` | The environment to resolve bindings in |
| `addresses` | The addresses the runtime supplies: `{"source": {"object", "export"}, "context": "container" or "host", "host", "port"}` |
| `equivalentTo` | Another case, as `<group>/<case>`, whose model has the same intent |
| `parity` | For SDK parity: `sdk` describes what the SDK half declares; `patches` lists the typed patches it applies, as `{"ref", "fields"}` |

A `module` case without `root` loads its single `packaged` repository module.

## What a runner checks

For a `project` case:

1. Load `root` with the `packaged` definitions. With `expected/diagnostics.json`, the sorted
   diagnostics must equal it and nothing else is checked.
2. Otherwise the encoded model must equal `expected/model.json` byte for byte.
3. `expected/model.json`, loaded as a Model root beside the case's inputs, must load without
   diagnostics and encode to itself; so must the encoder's own output.
4. With `equivalentTo`, both models' intent must be equal.
5. With `addresses`, resolved bindings must equal `expected/bindings.json`, or the resolution
   diagnostics `expected/resolution-diagnostics.json`.
6. `Unresolved` must equal `expected/unresolved.json`, or be empty.

A `semantics` case is a project that must load without diagnostics. The runner then calls
`validation.Validate`, which checks the effective options of each module instance at project level and in
each environment that overrides it. `Unchecked` must be empty and the sorted diagnostics must equal
`expected/diagnostics.json`, which is `[]` for a valid case. The files are written by hand from the rules in
[the PostgreSQL module](../../docs/modules/postgres.md#semantic-rules); a message never repeats a value
written under `options`.

A `module` case loads a Module Definition; a `bundle-lock` case loads both documents and checks the
Lock against the Bundle; a `version-info` case parses the document and encodes it back to the same
bytes; a `versions` case parses each version and requires the list to be in ascending precedence.

Across all cases, every diagnostic code except `reference.invalid_ref`, which only command-line
arguments produce, must appear in some expected diagnostics file (the codes of `validation/<module>/`
included), and no diagnostic message of an invalid or `semantics` case may repeat a value written under `environment`, an input's `value`, `options`,
`parameters` or `command`.

## Diagnostics files

A diagnostics file is a canonical JSON array of `{code, file, line, column, pointer}`, sorted by
file, line, column, pointer and code. `file` is relative to the case directory, or `""` for bytes
that have no file. `line` and `column` are omitted when unknown, and `column` alone is omitted when
only the line is known. Messages are not part of the contract.

## Parity with an SDK

A `parity` case is half of a pair: an SDK declares the same project in code and compares its output
with the expected files here.

- Intent, the Model without `provenance`, must be equal byte for byte.
- `provenance.*.declared` must be equal for declarations the SDK imports from YAML.
- The `fields` of each `patched` entry must be equal; an SDK's file, line and column are not
  compared.
- Diagnostics compare by code, file and pointer.

For a case with `patches`, `expected/model.json` is the state after the SDK's patches. The Go runner
checks that the YAML half loads, that the expected model is valid, and that the two agree outside the
patched fields.
