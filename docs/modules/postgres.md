# PostgreSQL configuration

The [schema](../../modules/postgres/content/configuration/schema.json) validates configuration for
one PostgreSQL instance. Instance selection belongs to the surrounding StackPuddle Configuration
document; this payload has no `instances`, `module`, `apiVersion`, or `kind` wrapper.

The configuration schema is implemented. The [module descriptor](../../modules/postgres/module.yaml),
[Compose runtime](../../modules/postgres/content/runtime/compose.yaml), and
[provisioning contract](../../modules/postgres/content/provisioning/contract.md) remain development
drafts. Compose starts the engine only. It does not consume this configuration or provision
application databases, users, grants, or TLS. Semantic validation, bindings, and CLI integration
are not implemented.

## Named access model

Three required nonempty maps describe access:

- `users`: login identities, keyed by their PostgreSQL role name.
- `roles`: reusable permission roles, also keyed by their PostgreSQL role name.
- `databases`: databases, ownership, connection access, schemas, and grants.

Each user requires `purpose` (`runtime` or `migrations`) and `passwordSecretRef`. Runtime users
require a nonempty, unique `memberOf` list of permission-role references. Migration users cannot
declare membership. Permission roles require `login: false` and accept no other fields.
Nested role membership, administrator flags, inline passwords, and raw SQL are not configurable.

A database requires `owner`, `connectRoles`, and `schemas`. The owner references a migration user;
`connectRoles` lists permission roles allowed to connect. Schema names are explicit, including
`public` when needed. Each schema requires a nonempty `grants` map keyed by permission role.

| Grant | Accepted privileges |
| --- | --- |
| `tables` | `SELECT`, `INSERT`, `UPDATE`, `DELETE` |
| `sequences` | `USAGE`, `SELECT` |

At least one grant list must be present; supplied lists must be nonempty and contain no duplicates.
The intended provisioner grants schema `USAGE` to each grantee and applies the declared object
privileges to existing objects and future objects created by the database owner in that schema.
Schema ownership and creation belong to the migration identity. Runtime users receive neither.
Function execution and row-security policies are outside these grant lists and remain application
migration responsibilities under the restrictions in the provisioning contract.

Membership privileges accumulate. A user assigned both reader and writer roles has writer access;
a reader role does not deny privileges inherited elsewhere. Use distinct permission roles when
databases need independent access. User and permission-role names share one PostgreSQL namespace.

## Instance and session settings

| Field | Accepted values |
| --- | --- |
| `resources.memoryMiB` | Integer, at least 256 MiB |
| `resources.cpus` | Positive number; fractions are allowed |
| `hostAccess.enabled` | Boolean; required when `hostAccess` is present |
| `hostAccess.port` | Integer from 1 to 65535; required when enabled |
| `startupTimeoutSeconds` | Integer from 1 to 3600 |
| `settings.maxConnections` | Integer, at least 4 |
| `settings.sharedBuffersMiB` | Integer, at least 1 MiB |
| `databases.<name>.connectionLimit` | Integer from -1 to 2147483647; -1 is unlimited, 0 blocks ordinary connections |
| `databases.<name>.settings` | Optional database session defaults |
| `databases.<name>.userSettings.<user>` | Optional overrides for a declared user in this database |

Both database settings and user overrides accept only `statementTimeoutMs` and `lockTimeoutMs`:
integers from 0 to 2147483647 milliseconds. Zero disables the timeout. Omitted fields request no
override. User settings must be applied with database scope, not globally to the login role.
These are session defaults, not permission boundaries; clients can change user-settable settings.
See [ALTER ROLE](https://www.postgresql.org/docs/current/sql-alterrole.html).

## Names and secrets

Names use 1–63 ASCII characters: a lowercase letter followed by lowercase letters, digits, or
underscores. The `pg_` prefix is reserved. Databases cannot be named `postgres`, `template0`, or
`template1`. Users and permission roles cannot be named `postgres`, `public`, `none`, `current_user`,
`session_user`, or `current_role`. Schemas can be named `public` but not `information_schema`.

Secret references are logical names, 1–128 characters long, using lowercase letters and digits
separated by single hyphens, beginning with a letter. They are not passwords, URLs, or file paths.
Unknown fields, unsupported privileges, and arbitrary engine settings are rejected.

## Examples and compatibility

- [One database](../../tests/contracts/testdata/modules/postgres/valid/minimal.yaml)
- [Multiple databases and scoped settings](../../tests/contracts/testdata/modules/postgres/valid/multiple-databases.yaml)
- [API, worker, and reporting users](../../tests/contracts/testdata/modules/postgres/valid/multiple-users.yaml)

This unpublished configuration replaces the earlier fixed `access.runtime` / `access.migrations`
shape. Move credentials into `users`, declare permission roles, and specify each database's owner,
connection roles, and schema grants. Move identity timeout overrides into database `userSettings`.
The old shape is rejected; no automatic conversion is provided. This change does not alter the
stable outer Manifest or Lock contracts. Module parser compatibility remains undeclared.

[Defaults](../../modules/postgres/content/configuration/defaults.yaml) supply 1024 MiB memory, one
CPU, disabled host access with port 5432, a 60-second startup timeout, 100 maximum connections, and
128 MiB shared buffers. They contain no access declarations. Consumers must supply `users`, `roles`,
and `databases`. Validation does not insert defaults or merge documents.

## Verification boundary

Contract tests exercise valid examples, required fields, privilege restrictions, membership shape,
name rules, scoped settings, and instance limits. Declaration counts do not establish server capacity.
The strict YAML test decoder rejects duplicate keys and aliases; the CLI parser is still planned.

JSON Schema validates structure, not references or effective access. Before any mutation, a future
semantic validator must reject:

- Unknown user or role references, and names shared between `users` and `roles`.
- A database owner whose purpose is not `migrations`, or one migration identity owning multiple
  managed databases.
- Grants to roles absent from the database's `connectRoles`.
- User overrides for identities without declared access to that database.
- Undeclared database connection access through direct grants, memberships, ownership, or `PUBLIC`,
  including built-in and unmanaged databases. Evaluate prospective access for new users; incomplete
  inspection must fail closed. Authentication/ACL changes outside the managed resources need a
  separate authorized plan.
- Missing secrets, unsafe existing memberships, ownership conflicts, and infeasible resource settings.

A schema-valid document is not authorization to provision. Existing privileges, live connections,
allow/deny behavior, and ownership checks require a running PostgreSQL instance and a provisioner.
Those checks remain unimplemented. Removing declarations must not silently delete resources.

```bash
GOWORK=off task check
```

## Planned inspection and migration bindings

The CLI is intended to list users and roles, explain effective permissions, and compare configured
access with database state. Explanations must distinguish direct grants, inherited grants, `PUBLIC`,
ownership, and role-switching authority. Future-object defaults and row-security policies must be
shown separately. Command syntax and output schemas are not implemented or frozen.

Application migration tools consume endpoint, database, migration identity, credential references,
and TLS settings through future bindings. Applications retain their migration scripts and history;
StackPuddle does not depend on Evolve, EF Core, or Flyway. Runtime bindings use separate credentials.
Administrator credentials must never be exported as application bindings.

## Compose development draft

These Compose files are development drafts, not a finished runtime adapter. TLS is not configured: the draft neither configures nor enforces it.

The image is PostgreSQL 18.6 on Debian Bookworm, pinned by a multi-platform image digest in Compose.
The data volume mounts at `/var/lib/postgresql`, matching the PostgreSQL 18 image layout. Changing
that image to another major version requires a reviewed upgrade procedure.

Run commands from the repository root with Docker Compose installed. Supply an existing, nonempty
administrator password file outside the repository through `STACKPUDDLE_POSTGRES_ADMIN_PASSWORD_FILE`.
The image uses it for initial bootstrap; changing it does not rotate an existing database password.
Keep the file private. It is distinct from the application's `passwordSecretRef` values.

The future CLI adapter must validate configuration and translate these values. The environment
variables below are the Compose calling convention, not another public configuration format.

| Configuration field | Compose input | Default |
| --- | --- | --- |
| `resources.memoryMiB` | `STACKPUDDLE_POSTGRES_MEMORY_MIB` | 1024 |
| `resources.cpus` | `STACKPUDDLE_POSTGRES_CPUS` | 1 |
| `settings.maxConnections` | `STACKPUDDLE_POSTGRES_MAX_CONNECTIONS` | 100 |
| `settings.sharedBuffersMiB` | `STACKPUDDLE_POSTGRES_SHARED_BUFFERS_MIB` | 128 |
| `hostAccess.port` | `STACKPUDDLE_POSTGRES_HOST_PORT` | 5432 |
| `hostAccess.enabled` | Select the host overlay only when true | false |
| `startupTimeoutSeconds` | Caller passes `--wait-timeout` | 60 |

Compose does not enforce the configuration schema or resource feasibility. Manual callers must
supply validated values. The fixed shared-memory mount is 128 MiB; additional tuning requires
runtime testing. Image bootstrap requires SCRAM authentication for TCP and peer authentication for
local sockets. Existing volumes retain their authentication configuration.

Render the base configuration without contacting the Docker daemon:

```bash
: "${STACKPUDDLE_POSTGRES_ADMIN_PASSWORD_FILE:?Set an existing administrator secret file}"
docker compose --project-name example-postgres --env-file /dev/null \
  --file modules/postgres/content/runtime/compose.yaml config --quiet
```

The startup command below is provided for development inspection; live startup is not yet verified.
Keep a stable, distinct project name for each environment and instance so storage remains separate.

```bash
docker compose --project-name example-postgres --env-file /dev/null \
  --file modules/postgres/content/runtime/compose.yaml \
  up --detach --wait --wait-timeout 60
```

To publish a localhost port, also select
`--file modules/postgres/content/runtime/compose.host.yaml` before the command. The base alone
publishes no port. The healthcheck probes TCP engine availability; it does not authenticate the
application identities or check their databases.

Stop the development instance while keeping its data volume:

```bash
docker compose --project-name example-postgres --env-file /dev/null \
  --file modules/postgres/content/runtime/compose.yaml down
```

Do not add `--volumes` when data must be retained. Resource deletion and major-version upgrades are
explicit operations, outside ordinary startup or configuration reconciliation.

The draft tests check descriptor references, archive construction, image pinning, volume scoping,
secret wiring, and rendered base/custom/localhost Compose models. Rendering tests skip if Docker
Compose is unavailable; source and archive checks still run. No rendering test starts a container.
