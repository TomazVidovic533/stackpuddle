# PostgreSQL acceptance fixture

The shared consumer fixture shape (`tests/fixtures/consumer/`, read, not edited) plus the jobs the lifecycle rows
need: `seed` writes one row as the migration user, `query` reads it back as the runtime user and exits 0 only over
TLS, and each `denied-*` job exits 0 only when the database refuses the operation with SQLSTATE 42501.
Two environments (`local`, `second`) each expose the PostgreSQL Export on their own loopback port. `unsupported/`
requires a capability this engine does not advertise.
