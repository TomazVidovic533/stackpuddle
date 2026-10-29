// Package schemas serves the published stackpuddle JSON Schemas.
package schemas

import (
	"embed"
	"io/fs"
)

// The schema files are the contract; embedding them keeps one copy that every reader compiles.
//
//go:embed v1/*.schema.json
var files embed.FS

// Files returns the embedded schema files as "v1/<stem>.schema.json". Safe for concurrent use.
func Files() fs.FS {
	return files
}
