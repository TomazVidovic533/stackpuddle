// Package validation runs the pure semantic validator of each module a project uses. The catalog
// lists every validator explicitly; nothing registers itself. Module semantics live behind the
// module boundary: this package holds the Go rules, modules/<name>/ holds the module content, and
// neither depends on the other's directory. Every caller that checks a project uses this package
// rather than a copy.
package validation
