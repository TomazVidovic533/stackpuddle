# ADR-0007: Compatibility representation

- Status: Proposed
- Date: 2026-10-29

## Context

The CLI, its engine, module archives, bundles, project files and SDKs are released on their own
schedules. A reader must refuse what it cannot read before it touches a runtime, and development
builds carry pseudo-versions that sort below every release.

## Decision

Compatibility is declared by contract, not by product version.

- Readers declare what they read: the model library its API versions; the engine its state versions
  and its capabilities, such as `module.postgres/1` or `runtime.compose/1`.
- Artifacts declare what they need. A module lists `requires.capabilities` and its `dataFormat`, the
  format of the data it keeps, such as `postgresql/18`. A bundle lists its tested set: one exact
  version per module.
- `VersionInfo` publishes what a build is and what it reads.
- An unknown `apiVersion` fails with `compatibility.unsupported_api_version` and an unknown `kind`
  with `compatibility.unsupported_kind`; each names the supported set. A Lock module outside its
  bundle's tested set fails with `compatibility.module_not_in_bundle`, and a Lock that names another
  bundle version with `compatibility.bundle_version_mismatch`.
- Versions order as SemVer 2.0.0 says, so prerelease identifiers compare numerically.
- No CLI version range is published until releases are tagged.

## Rejected alternatives

- A supported-CLI range in bundles now: every development build is a pseudo-version below any
  release, so the range would reject all of them.
- Readers that tolerate unknown fields: an older reader would drop what it does not understand and
  act on the rest.

## Consequences

- Every capability identifier is a public name that is kept or retired explicitly.
- A data-format change is never applied silently; it needs an explicit migration.

## Verification

Conformance cases under `tests/conformance/cases/` cover an unknown API version, an unknown kind, a
module outside its bundle, a bundle version mismatch and prerelease ordering.
