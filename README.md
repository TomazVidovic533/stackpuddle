# StackPuddle

Define your application stack in code.

StackPuddle is a developer platform being designed for reproducible local environments, supporting infrastructure, and cloud deployment. Docker Compose is the first planned local runtime, with PostgreSQL as the first infrastructure module.

This repository is the home for shared schemas, reusable infrastructure modules, recipes, tested bundles, examples, and project documentation. The Go execution engine lives in [StackPuddle Control Plane](https://github.com/TomazVidovic533/stackpuddle-control-plane), and the [StackPuddle CLI](https://github.com/TomazVidovic533/stackpuddle-cli) will run it in-process.

## Status

Early development. No installable release is available yet.

## What is here today

- [`artifacts`](artifacts): a Go package that builds deterministic tar+gzip module and bundle archives and inspects their structure and size limits without extracting them. Inspection does not validate descriptor schemas or publisher identity.
- [`schemas/v1`](schemas/v1): JSON Schemas for the stable `stackpuddle/v1` Manifest and Lock documents and their shared definitions. The Module, Bundle, Configuration, and Bindings documents are planned; see [ADR-0001](docs/architecture/decisions/0001-stable-v1-contract-model.md).
- [`modules/postgres`](modules/postgres) `0.1.0-alpha.1`: a configuration schema and defaults covered by contract tests. The module descriptor, provisioning contract, and PostgreSQL Compose files are development drafts, not a finished runtime adapter. Nothing provisions databases, users, or grants yet, and TLS is not configured. See [PostgreSQL configuration](docs/modules/postgres.md).

Run the checks with Go 1.27.0 or later, Task 3, and a C compiler (the race detector needs cgo). Docker Compose enables the Compose rendering tests; without it they are skipped.

```bash
GOWORK=off task check
```

`task test:acceptance` fails on purpose until packaged consumer acceptance exists.

Architecture decisions are recorded in [docs/architecture/decisions](docs/architecture/decisions/README.md).

## Planned authoring SDKs

- [TypeScript](https://github.com/TomazVidovic533/stackpuddle-typescript)
- [.NET](https://github.com/TomazVidovic533/stackpuddle-dotnet)
- [Python](https://github.com/TomazVidovic533/stackpuddle-python)
- [Java](https://github.com/TomazVidovic533/stackpuddle-java)

## License

Licensed under the [Apache License 2.0](LICENSE).
