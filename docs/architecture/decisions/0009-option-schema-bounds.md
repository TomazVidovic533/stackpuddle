# ADR-0009: Option-schema dialect and evaluation bound

- Status: Proposed
- Date: 2026-10-29

## Context

A module's option schema is untrusted input (a project, a pull request or a module publisher
writes it), and every options check validates against it. JSON Schema validation is exponential in
the depth of chained `anyOf`/`oneOf`/`allOf` over shared references, and polynomial in branches
times instance values even without references. Measured through `model.Load`: 22 chained `$defs`
(1,672 bytes) took 10 to 12 s and 5.6 GiB; 600 failing `const` branches against 50,000 options took
18.7 s. The 2,048-value cap on the schema as written did not bound either.

## Decision

- Dialect: an option schema is JSON Schema 2020-12: the compiler defaults to that draft and a
  `$schema` naming any other draft is refused, as is a list-valued `items` or `additionalItems`
  (older drafts read those as subschemas the size count would miss). An option schema may use only
  `#/$defs/<name>` references to its own top-level `$defs`; a `%` in a reference is refused, since
  the validator percent-decodes the name.
  References must form no cycle; `$dynamicRef`, `$dynamicAnchor`, `$anchor`, `$recursiveRef` and a
  nested `$id` are refused (`structure.invalid_schema`). Our `modules/postgres` schema fits.
- Size: the schema counted with every reference expanded, memoised per definition, must not pass
  `maxOptionSchemaValues` (2,048), else `syntax.limit_exceeded` at the schema file.
- Work: one Load has a budget of `maxOptionCheckUnits` (2^23). Each options check is charged the
  instance's value count times the expanded schema size; the check that would pass the budget is
  refused with `syntax.limit_exceeded` at the instance or override, as `maxOverrideValues` does.
  `ModuleDefinition.ValidateOptions` applies the budget to one call.
- Both numbers are named defaults, fixed in code, not configurable.

## Rejected alternatives

- Documenting the risk: leaves CI, the CLI and the control plane's single reconciler stoppable by
  a 2 KB file.
- Banning `$ref` and `$defs`: breaks our own postgres schema and forces copies of shared shapes.
- A wall-clock timeout: the result would depend on the machine, and the work cannot be cancelled
  inside the validator.

## Consequences

- Recursive option schemas (`"$ref": "#"`) are no longer accepted.
- A very large project may reach the budget and must split its modules.

## Verification

`tests/unit/model/schemacheck_expand_test.go` covers the dialect, the draft pin, tuple-form `items`,
percent-encoded references and the chained schema at 30 and 40 levels; `tests/contracts/model/allocation` refuses the chained and the many-branches cases with
`syntax.limit_exceeded` within an allocation ceiling and a time bound.
