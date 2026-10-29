# The packaged local journey

This page is the journey from a packaged CLI, the PostgreSQL module archive and the TypeScript SDK tarball to a
running local environment. Every `yaml stackpuddle.yaml` block below is loaded by
`tests/contracts/journey_snippets_test.go` with no diagnostics. The Docker-backed run of the same journey is
`tests/acceptance/packaged`; its results are not claimed here until it has passed on a machine with Docker.

## 1. Declare the project

```yaml stackpuddle.yaml
apiVersion: stackpuddle/v1
kind: Project
metadata:
  name: journey
spec:
  modules:
    db:
      definition: postgres
      options:
        users:
          app_runtime:
            purpose: runtime
            passwordSecretRef: app-runtime-password
            memberOf: [app_readwrite]
          app_migrator:
            purpose: migrations
            passwordSecretRef: app-migrator-password
        roles:
          app_readwrite:
            login: false
        databases:
          app:
            owner: app_migrator
            connectRoles: [app_readwrite]
            schemas:
              public:
                grants:
                  app_readwrite:
                    tables: [SELECT, INSERT, UPDATE, DELETE]
                    sequences: [USAGE, SELECT]
  environments:
    local:
      runtime:
        type: compose
        context: default
      secrets:
        provider: local
```

## 2. Check, then bring it up

```bash
stackpuddle check --env local
stackpuddle up --env local
stackpuddle stop --all --env local
stackpuddle destroy env/local --delete-data --env local
```

`up` prints the accepted operation ID. An operation stopped by Ctrl-C or a closed terminal is `interrupted`; the
next `up` continues under a new ID that names the old one.
