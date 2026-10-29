# ADR-0006: Schema identifiers and document kinds

- Status: Proposed
- Date: 2026-10-29

## Context

StackPuddle documents are read by Go code, SDKs and editors. Each needs one stable name for its
schema and one way to recognise what a document is before the rest is parsed.

## Decision

- Every document starts with `apiVersion: stackpuddle/v1` and a PascalCase `kind`. A reader checks
  both first and refuses a document that lacks either.
- The v1 kinds are `Manifest` and `Lock` (ADR-0001), and `Project`, `Fragment`, `Model`, `Module`,
  `Bundle`, `Bindings` and `VersionInfo`. Adding a kind is an additive v1 change.
- The schema for a kind lives at `schemas/v1/<stem>.schema.json` with `$id`
  `urn:stackpuddle:schema:v1:<stem>`, where `<stem>` is the kind in kebab case (`version-info` for
  `VersionInfo`). Shared rules live in `urn:stackpuddle:schema:v1:definitions`.
- A module's own option schema lives in its content with `$id`
  `urn:stackpuddle:module:<module>:configuration`.
- Schemas compile from the files the library embeds and from the module's own option schema, with no
  network or file access.
- ADR-0001 plans Configuration and Bindings documents. In v1, the values a project supplies to a
  module are the module instance's `options` inside the Project and Model documents, validated by
  the module's option schema. The values a module exposes for consumption are the `Bindings`
  document. No separate Configuration document exists, so option values have one home.

## Rejected alternatives

- `https://` identifiers: they suggest a fetchable location and invite network loading.
- Version numbers in file names: the `v1` folder already carries the version.
- A standalone Configuration document beside Project: option values would have two homes that can
  disagree.

## Consequences

- A new kind adds a schema file and an embedded resource; existing kinds are unchanged.
- A second API version gets its own folder, `schemas/v2/`, and its own identifiers.

## Verification

`tests/contracts/model_schemas_test.go` compiles every schema with loading disabled, checks each
`$id` against its file name and `kind`, and checks that the embedded set equals `schemas/v1/`.
