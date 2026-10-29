# ADR-0001: Stable v1 StackPuddle contract model

- Status: Accepted
- Date: 2026-09-11
- Realization: Partial. Manifest and Lock are implemented and verified. Module, Bundle,
  Configuration, and Bindings are planned.
- Amended: 2026-10-28. The API version and schema namespace were renamed to `stackpuddle/v1` and the product name to StackPuddle; the decision is otherwise unchanged.

## Context

Applications need a shared contract for declaring platform capabilities and reproducing the exact
artifacts selected from that declaration. Contract documents must evolve without making existing
consumers reinterpret an established version.

## Decision

Use six documents under the stable `stackpuddle/v1` API version. Each document has one
responsibility.

| Document | Responsibility |
| --- | --- |
| Manifest | Declares the bundle version and selected module names requested by an application. |
| Lock | Records the exact bundle and module artifact versions, sources, formats, and SHA-256 digests. |
| Module | Describes one independently versioned platform capability and its compatibility requirements. |
| Bundle | Defines a tested set of compatible module versions. |
| Configuration | Holds application-provided values for selected modules. |
| Bindings | Describes values exposed by installed modules for application consumption. |

The [Manifest schema](../../../schemas/v1/manifest.schema.json) contains intent only. The
[Lock schema](../../../schemas/v1/lock.schema.json) contains resolved artifact identity. Shared
field rules live in [the v1 definitions schema](../../../schemas/v1/definitions.schema.json).

Contract tests use a strict YAML fixture decoder that rejects ambiguous YAML constructs and values
that cannot be represented in JSON. This decoder is test infrastructure for proving the JSON Schema
contracts; it is not the StackPuddle CLI runtime parser. CLI recognition and runtime parsing of stable
`v1` documents remain planned work.

`v1` is a compatibility commitment. Existing `v1` fields and meanings remain supported. A breaking
contract change requires a new API version; additive changes must preserve existing valid documents
and consumer behavior.

## Rejected alternatives

- `v1alpha1`: rejected because these private repositories have no external consumers and the initial
  contract is intended to establish the supported compatibility boundary now.
- Combined intent and lock data: rejected because user choices and resolved artifact identity have
  different lifecycles. Keeping them separate allows repeatable resolution without rewriting intent.
- Coupled component versions: rejected because bundles, modules, and the CLI need independent release
  cycles. Compatibility belongs in explicit contracts rather than matching version numbers.

## Consequences

- Breaking changes require a new API version and parallel schema support during migration.
- Manifest and Lock remain separate even when a tool reads or writes both in one operation.
- Bundle, module, and CLI releases may advance independently when their declared compatibility allows it.
- Stable `v1` limits speculative fields: additions need a demonstrated contract requirement and
  compatibility analysis.

## Affected contracts

Implemented:

- [Manifest](../../../schemas/v1/manifest.schema.json)
- [Lock](../../../schemas/v1/lock.schema.json)
- [Shared definitions](../../../schemas/v1/definitions.schema.json)

Planned:

- Module
- Bundle
- Configuration
- Bindings

## Verification evidence

The contract test suite compiles the JSON Schemas, validates positive and negative fixtures, checks
every required field, and verifies that mapping and Manifest module ordering do not affect validity.
The fixture decoder rejects unsupported YAML and non-JSON numeric values. Negative contract tests
assert both the instance location and schema keyword so failures prove the intended contract rule.

```bash
task verify
```
