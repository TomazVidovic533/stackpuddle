# ADR-0002: Module and bundle archive layout

- Status: Proposed
- Date: 2026-10-29

## Context

Module content reaches the CLI and the engine as an archive. Every reader must agree on what an
archive may contain, in which order and with which metadata, so that one set of bytes has one
identity and extraction writes nothing unexpected.

## Decision

A module archive is a tar stream compressed with gzip. Its first entry is the regular file
`module.yaml`; every other entry is `content` or a path beneath `content/`, in byte order. A bundle
archive holds only `bundle.yaml`. Entries are USTAR regular files or directories with uid and gid 0,
empty owner names, modification time 0, and mode 0644, or 0755 for directories and executable
content files; the descriptor is never executable. The gzip header carries no name, comment or extra
field, modification time 0, operating system 255 and the best-compression flag. Trailing bytes, a
second gzip member and trailing tar blocks are rejected.

Inspection validates the stream within declared limits before anything is extracted. It rejects
entry names that are unsafe to extract on common filesystems: names that differ only by case,
Windows device names, names containing `:` and names containing control characters. Archives carry
no Go code.

This confirms the layout the archive code enforces (`internal/artifacts/envelope/layout.go:16-24`,
`:47-53`; `internal/artifacts/targzip/builder.go`;
`internal/artifacts/targzip/ustar_validation.go`).

## Rejected alternatives

- Zip archives: the central directory sits at the end, so a reader cannot validate a stream without
  buffering it, and zip has no canonical metadata form.
- OCI artifacts in a registry now: they add a registry and its credentials before any release
  exists. The tar+gzip payload can later travel as an OCI layer unchanged.
- A free layout: any top-level file would be legal, so extraction could write files nobody reviewed.

## Consequences

- Limits only loosen within v1, because tightening them would reject archives a Lock already pins.
- Go does not promise identical compressed output across releases, so identity is the digest of
  published bytes, never of a rebuild (ADR-0004).
- Go source cannot ship inside a module archive.

## Verification

`tests/contracts/artifacts/`, `tests/filesystem/artifacts/` and the `FuzzInspect` target cover
layout, metadata, ordering, limits and unsafe names.
