# PostgreSQL provisioning contract

Development proposal. No provisioner implements this contract yet. This document is an archive
reference, not an executable hook or permission to run arbitrary scripts.

## Inputs and preflight

The caller supplies an installation identity, configuration validated structurally and semantically,
an administrator connection, and resolved user credentials. Administrator credentials are separate
from consumer credentials. Application migration scripts are not provisioning inputs.

Before any database mutation:

- Validate the complete configuration and resolve every secret reference without logging values.
- Reject names shared by users and permission roles, or collisions with administrator roles.
- Resolve every membership, owner, connection-role, schema-grantee, and user-settings reference.
- Require runtime memberships to target declared permission roles only. Migration users and
  permission roles have no configurable memberships; reject conflicting existing membership paths.
- Require each database owner to have purpose `migrations`. A migration identity owns one managed
  database; applications needing separate databases use separate migration identities.
- Require each schema grantee to appear in that database's `connectRoles`.
- Require each user-settings target to be the owner or a runtime user with membership in at least
  one connection role for that database.
- For every managed login, inspect effective `CONNECT` on every connectable database, including
  `postgres`, `template1`, and unmanaged databases. Migration users may access only their owned
  database; runtime access follows declared connection-role memberships. Include direct, inherited,
  ownership, and `PUBLIC` access. For new logins, evaluate prospective privileges before creation.
  Refuse provisioning before mutation if undeclared access exists or inspection is incomplete.
  Enforcing a different connection boundary requires a separately authorized authentication/ACL
  plan; never revoke `PUBLIC` or change unrelated databases implicitly.
- Check memory and connection settings against the instance budget and supported engine limits.
- Check existing resource ownership against installation state; reject unowned name collisions.
- Produce the intended changes, including restarts and privilege changes, before applying them.

User/role names are instance-wide; databases and schemas do not create separate role namespaces.
Multiple runtime identities can share a permission role, including across databases when explicitly
configured. Effective privileges are the union of all grants and memberships, including `PUBLIC`.
Schema validation cannot enforce these reference and state checks. Their implementation is required
before this contract can authorize provisioning.

Installation state must identify owned resources and completed operations before reconciliation can
adopt them. State storage and recovery belong to the future CLI design. A matching name alone does
not prove ownership.

## Access and ownership

| Identity | Authority |
| --- | --- |
| Administrator | Reconciles resources; never returned in application bindings |
| Migration user | Owns its database and declared schemas; creates application objects |
| Runtime user | Logs in and inherits declared permission-role privileges |
| Permission role | Cannot log in; holds explicit database, schema, and object grants |

All managed consumer and permission roles lack superuser, database-creation, role-creation,
replication, and row-security-bypass attributes. Runtime users cannot own application objects,
inherit a migration/admin identity, or switch to one. Runtime membership grants allow ordinary
privilege inheritance but not membership administration or role switching. The application connects
as its own login identity; explicit `SET ROLE` support is outside this version.

For each newly managed database:

1. Assign ownership to its migration user. Revoke `PUBLIC` connection and temporary-object rights.
2. Grant `CONNECT` to the owner and the declared `connectRoles`. Do not grant runtime `CREATE` or
   `TEMPORARY` rights.
3. Create declared schemas owned by that migration user. Remove public creation rights from the
   default `public` schema; remove `PUBLIC` access from managed application schemas before grants.
4. Grant schema `USAGE` to each declared grantee. Grant exactly the requested table and sequence
   privileges on managed objects in that schema, without grant options.
5. Establish matching default privileges for objects created by the migration user in each schema.
   Defaults affect future objects only; existing objects require separate grants.

Table grants cover tables and views; sequence privileges are separate. Ownership changes, default
privileges, or a matching schema name must not silently adopt objects created by another owner.
Application migrations must create objects as the configured owner for the defaults to apply.

Function execution is not configurable in this model. Provisioning must remove `PUBLIC` execution
from managed application routines and set the owner's default routine privileges to omit `PUBLIC`.
Explicit function grants and row-security policies belong to application migrations and must not
provide a path around runtime restrictions. Application tests must cover those paths. Superuser and
owner connections are not valid substitutes for runtime identities in access tests.

## Reconciliation

Create missing owned resources and reconcile declared privileges, connection limits, and timeouts.
Use correct SQL identifier quoting and value binding after validation. Database creation is not
transactional with all other provisioning steps.

Database timeout settings use database session defaults. `userSettings` overrides are scoped to that
user in that database. Reset only previously managed overrides that disappear; preserve unrelated
settings. New sessions receive defaults, and clients may change user-settable settings.

Repeated runs preserve data, resource identity, and credentials. Changing a secret reference or its
value requires an explicit rotation operation. Ownership or effective-permission drift requires a
specific conflict or reviewed repair plan. Privilege additions, removals, and membership changes
must be visible in that plan. Reconciliation must not silently broaden access or revoke unrelated
application-managed grants.

Record partial completion so retries can resume safely. Failure after creating one database must
not delete it or another application's data. Removing a database, user, role, or schema declaration
does not automatically delete the resource. Credential revocation and resource deletion are explicit
operations with their impact reviewed before execution.

## Outputs and inspection

Return per-user database, username, endpoint, and credential references. Distinguish host and
container endpoints; omit host endpoints when publication is disabled. TLS bindings will require
verified trust and hostname settings once TLS support is implemented. The current Compose draft
neither configures nor enforces TLS. Reject unsupported TLS configuration rather than implying it
is active.

Migration bindings are restricted to migration jobs. Runtime consumers receive their own identity.
Never expose administrator credentials or password-bearing connection strings in output or logs.
Application tooling owns migration ordering, history, and recovery; provisioning does not execute
Evolve, EF Core, Flyway, or product SQL scripts.

Future read-only CLI inspection must list users and roles and explain effective access through
direct grants, memberships, `PUBLIC`, ownership, and role-switching authority. Report default
privileges and row-security policies separately. Compare configured and observed access without
repairing drift during inspection. Missing inspection authority is an incomplete result, not proof
that access is absent. No inspection commands are implemented by this contract.

## Runtime acceptance

Before provisioning is accepted, authenticated tests must prove:

- The migration identity can create application objects; runtime identities cannot perform DDL.
- Writer users can perform only their declared DML; reader users can select but cannot write.
- Two users can share a permission role without sharing credentials.
- New migration-created tables and sequences receive the intended defaults.
- Runtime users cannot inherit or assume migration/admin roles. Managed logins cannot connect to
  undeclared managed, built-in, or unmanaged databases, using authenticated denial tests.
- User timeout overrides apply only in the declared database.
- A second database can be added, repeated runs preserve state, and partial failures can be retried.
- Restart preserves data; rejected changes leave existing resources intact.

A successful engine readiness probe or schema validation does not prove these properties. All live
provisioning and access checks remain pending. TLS acceptance will also need authenticated
connections with certificate verification and rejection of plaintext and invalid certificates.
