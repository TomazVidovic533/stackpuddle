// Package postgres holds the pure semantic rules of the postgres module's options: the
// relationships between users, roles, databases, schemas and settings that the module's JSON Schema
// cannot express. It reads bytes and returns diagnostics; it never touches the file system, the
// network, the clock or the environment.
//
// The package sits outside modules/, whose archive layout rejects Go files. "Module semantics live
// in modules" means the module boundary, not that directory: this package is the postgres module's
// Go half, and modules/postgres/ is its content half.
package postgres
