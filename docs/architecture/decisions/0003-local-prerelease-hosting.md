# ADR-0003: Local prerelease hosting

- Status: Proposed
- Date: 2026-10-29

## Context

Module and bundle archives need a source a Lock can name before any release channel exists, without
opening a path for unverified bytes.

## Decision

An artifact source in a Lock is an `https://` URL with no user information, query or fragment, or an
`http://127.0.0.1` URL with an optional port for prerelease archives served on the same machine
(`schemas/v1/definitions.schema.json:14-21`). No other plain-HTTP host and no `file:` URL is
accepted. A prerelease carries a SemVer prerelease version such as `0.1.0-alpha.1`, and versions
order as SemVer 2.0.0 says: numeric identifiers compare as numbers, so `alpha.9` sorts before
`alpha.10`. The transport never establishes identity; the Lock digest does (ADR-0004).

## Rejected alternatives

- `file:` sources: they bypass the verified cache and tie a Lock to one checkout's layout.
- Plain HTTP to other hosts: no transport protection, and it invites shared LAN hosting that other
  machines would come to depend on.
- A hosted registry now: it needs credentials and an operator before any release exists.

## Consequences

- A Lock that names a loopback source works only on the machine that serves it; it is a development
  pin, not a shareable one.
- Release hosting is a later decision. It changes the source, not the digest.

## Verification

`tests/contracts/lock_schema_test.go` accepts both source forms and rejects remote HTTP, user
information, queries and fragments. The `versions` conformance case checks prerelease ordering.
