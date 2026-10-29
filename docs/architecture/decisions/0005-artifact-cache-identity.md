# ADR-0005: Artifact cache identity

- Status: Proposed
- Date: 2026-10-29

## Context

The CLI keeps downloaded archives so repeated runs are fast and work offline. During development the
content behind one version changes, so a cache keyed by version would serve stale bytes.

## Decision

The cache is keyed by digest only: `<cache root>/artifacts/sha256/<hex>.tar.gz`, where `<hex>` is
the lowercase SHA-256 of the compressed bytes. The cache root is the user's cache directory, and
`STACKPUDDLE_CACHE_DIR` is its one override. An entry is written to a temporary file, verified
against its digest, then renamed into place. No entry is used without that check, and a failed
verification leaves the last verified entry untouched.

## Rejected alternatives

- Keying by name and version: a development build keeps its version while its content changes.
- A cache inside the project checkout: cleaning the checkout or using a second one refetches
  everything, and the cache shows up as untracked files.

## Consequences

- Identical bytes are stored once, whatever version names them.
- The cache grows until it is pruned; pruning is a later operation.

## Verification

Verified with the CLI's artifact resolution, which writes and reads this layout.
