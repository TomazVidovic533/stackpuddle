# StackPuddle

Define your application stack in code.

StackPuddle is a developer platform being designed for reproducible local environments, supporting infrastructure, and cloud deployment. Docker Compose is the first planned local runtime, with PostgreSQL as the first infrastructure module.

This repository is the home for shared schemas, reusable infrastructure modules, recipes, tested bundles, examples, and project documentation. The Go execution engine lives in [StackPuddle Control Plane](https://github.com/TomazVidovic533/stackpuddle-control-plane), and the [StackPuddle CLI](https://github.com/TomazVidovic533/stackpuddle-cli) will run it in-process.

## Status

Early development. No installable release is available yet.

## What is here today

- [`artifacts`](artifacts): a Go package that builds deterministic tar+gzip module and bundle archives and inspects them as a stream within size limits, without extracting them. Inspection rejects entry names that are unsafe to extract; it does not validate descriptor schemas or publisher identity.
- [`model`](model): a Go package that parses `stackpuddle/v1` YAML into a versioned project model, reports source-located diagnostics with stable codes, and writes the canonical JSON model. It reads only the files it is given and provisions nothing. See [the model contract](docs/model.md).
- [`validation`](validation): a Go package that runs each module's pure semantic validator over a loaded project, at project level and in each environment that overrides it, and reports source-located diagnostics with stable codes. [`validation/postgres`](validation/postgres) holds the PostgreSQL rules, such as unknown references and migration ownership. It changes nothing. See [PostgreSQL configuration](docs/modules/postgres.md#semantic-rules).
- [`schemas/v1`](schemas/v1): JSON Schemas for the `stackpuddle/v1` documents (Project, Fragment, Model, Module, Bundle, Bindings, VersionInfo, Manifest, Lock and Operation) and their shared definitions. Operation is the public record of an engine operation, which `inspect operation/<id> --output json` prints. The [`schemas`](schemas) Go package embeds them.
- [`modules/postgres`](modules/postgres) `0.1.0-alpha.1`: a configuration schema and defaults covered by contract tests. The module descriptor, provisioning contract, and PostgreSQL Compose files are development drafts, not a finished runtime adapter. Nothing provisions databases, users, or grants yet, and TLS is not configured. See [PostgreSQL configuration](docs/modules/postgres.md).
- [`tests/conformance`](tests/conformance): reviewed valid and invalid cases with expected models and diagnostics, shared with the authoring SDKs.
- [`docs/journey.md`](docs/journey.md): the local journey as snippets. Each project snippet is loaded by a contract test with no diagnostics; the Docker-backed run of the journey is not yet verified.

Run the checks with Go 1.27.0 or later, Task 3, and a C compiler (the race detector needs cgo):

```bash
GOWORK=off task check               # needs no Docker
GOWORK=off task test:integration    # needs Docker Compose, and fails by name without it
GOWORK=off task check:supply-chain  # needs network access to the Go vulnerability database
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
