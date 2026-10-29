# ADR-0004: Bootstrap trust rule

- Status: Proposed
- Date: 2026-10-29

## Context

The first fetch of an artifact has no earlier copy to compare with. Something must decide which
bytes are right before any are inspected or used.

## Decision

The committed Lock is the trust root. It pins every module and bundle archive by the SHA-256 digest
of its compressed bytes (`schemas/v1/definitions.schema.json:13`; `schemas/v1/lock.schema.json`). A
reader computes the digest of fetched bytes and compares it with the Lock before it inspects, caches
or extracts them. A mismatch fails and keeps any previously verified copy. Changing an artifact is a
Lock change, reviewed like other source. A digest proves identity only, not that the content is safe
or who may run it.

## Rejected alternatives

- Trust on first use without a Lock: whatever the source serves on the first day is trusted from
  then on.
- Trusting a version or tag: one version can be republished with other bytes.
- Signatures now: they need publisher keys and a verification policy before any release exists. A
  signature can be added beside the digest later without changing this rule.

## Consequences

- Every artifact change shows as a Lock diff.
- Inspection (ADR-0002) runs on bytes that already match a pinned digest, so its limits defend
  against mistakes and hostile pins rather than substitution.

## Verification

The Lock schema tests reject a module without a digest and a floating version
(`tests/contracts/lock_schema_test.go`). The digest check before use is verified where the CLI
resolves artifacts.
