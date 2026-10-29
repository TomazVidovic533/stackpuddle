// Package model parses stackpuddle/v1 project files into a checked, versioned project model and
// writes its canonical JSON form.
//
// Load reads a Project root with its Fragment imports, or a canonical Model file, through an fs.FS
// the caller confines. Checking runs in fixed stages and reports Diagnostics with stable codes, file
// and JSON-pointer locations and source positions. Messages name pointers and identifiers only,
// never declared values. Encode writes the Model document every authoring path shares, and
// ResolveBindings turns runtime-supplied addresses into the endpoint each consumer reaches.
//
// The package performs no network, process or clock access and provisions nothing. Every value it
// returns is immutable and safe for concurrent use.
package model
