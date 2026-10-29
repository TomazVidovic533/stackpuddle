# Shared consumer fixture

A project with a postgres module instance `db`, a migration job `migrate` that binds the module as the
migration user, and a service `app` that binds it as the runtime user and requires the job. Each binding
delivers its endpoint as `PGHOST`, `PGPORT`, `PGDATABASE` and `PGUSER`, and its password and the CA as
files; the scripts connect with `sslmode=verify-full`.

Environments: `local`, and `failing-migration`, which overrides one `migrate` value so the job exits 3.

Tests in other repositories read this folder from the sibling tree; changing it is breaking for them.
The project name is a placeholder that a test replaces with its own disposable name.
