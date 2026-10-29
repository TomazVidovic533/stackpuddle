# ADR-0008: Deletion semantics

- Status: Proposed
- Date: 2026-10-29

## Context

Removing a declaration, stopping an environment and destroying data are different intentions. A tool
that treats one as the next loses data.

## Decision

- Removing a declaration changes intent only. It never deletes runtime resources or data.
- Stopping keeps every volume and its data.
- Destroying deletes only runtime resources StackPuddle owns that are selected explicitly, and
  deletes persistent data only with explicit data-deletion intent.
- Persistent volumes retain their data by default; v1 declares no automatic deletion policy.
- A reference to an external resource never stops, upgrades or deletes it.

## Rejected alternatives

- Garbage-collecting resources whose declaration disappeared: one bad edit or a checkout of the
  wrong branch would delete data.
- Destroying on removal: the safe action and the destructive one would be the same command.

## Consequences

- Owned resources whose declaration was removed stay until they are destroyed explicitly; inspection
  lists them.
- Disposable data policies are a later, explicit addition.

## Verification

The v1 schemas offer no deletion policy field. Lifecycle acceptance verifies stop, remove and
destroy against a real runtime once those commands exist.
