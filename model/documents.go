package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
	"github.com/TomazVidovic533/stackpuddle/internal/model/yamlvalue"
	"github.com/TomazVidovic533/stackpuddle/schemas"
)

// Parser limits bound the work one untrusted file can cause: its size, nesting, key length and value
// count bound the memory and time of every later stage, and the diagnostic cap bounds the report.
const (
	maxFileBytes = 1 << 20
	maxDepth     = 64
	// maxKeyBytes takes YAML's 1024-character limit on implicit keys, counted in bytes, to explicit
	// "? key" keys, which YAML leaves unbounded.
	maxKeyBytes = 1024
	maxValues   = 1 << 16
	// maxFileDiagnostics keeps a file with a fault in every member to a report a person can read.
	maxFileDiagnostics = 100
	schemaPrefix       = "urn:stackpuddle:schema:v1:"
	schemaDir          = "v1"
)

func parserLimits() yamlvalue.Limits {
	return yamlvalue.Limits{
		FileBytes: maxFileBytes, Depth: maxDepth, KeyBytes: maxKeyBytes, Values: maxValues, Violations: maxFileDiagnostics,
	}
}

// The v1 document kinds.
const (
	kindManifest    = "Manifest"
	kindLock        = "Lock"
	kindProject     = "Project"
	kindFragment    = "Fragment"
	kindModel       = "Model"
	kindModule      = "Module"
	kindBundle      = "Bundle"
	kindBindings    = "Bindings"
	kindVersionInfo = "VersionInfo"
)

// schemaStems maps each kind to its schema file stem: the kind in kebab case.
func schemaStems() map[string]string {
	return map[string]string{
		kindManifest: "manifest", kindLock: "lock", kindProject: "project", kindFragment: "fragment",
		kindModel: "model", kindModule: "module", kindBundle: "bundle", kindBindings: "bindings",
		kindVersionInfo: "version-info",
	}
}

func knownKinds() []string { return slices.Sorted(maps.Keys(schemaStems())) }

// document is one decoded file and where its members sit.
type document struct {
	file      string
	tree      any
	positions yamlvalue.Positions
	kind      string
}

// at returns the location of pointer in the document; an absent member resolves to the position of
// its nearest present ancestor.
func (d *document) at(pointer string) Location {
	position := d.positions.Locate(pointer)
	return NewLocation(d.file, pointer, position.Line, position.Column)
}

// subtree returns the value at pointer, if present.
func (d *document) subtree(pointer string) (any, bool) {
	node := d.tree
	if pointer == "" {
		return node, true
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		child, present := childOf(node, token)
		if !present {
			return nil, false
		}
		node = child
	}
	return node, true
}

// childOf returns the member token names: a key of an object or an index of an array.
func childOf(node any, token string) (any, bool) {
	switch container := node.(type) {
	case map[string]any:
		child, present := container[token]
		return child, present
	case []any:
		index, err := strconv.Atoi(token)
		if err != nil || index < 0 || index >= len(container) || strconv.Itoa(index) != token {
			return nil, false
		}
		return container[index], true
	default:
		return nil, false
	}
}

// decode reads the schema-valid tree into a wire struct, refusing members the struct lacks, so the
// Go types are held to the schema by every valid conformance case.
func (d *document) decode(out any) Diagnostics {
	value, err := canonical.FromTree(d.tree)
	if err == nil {
		err = decodeStrict(canonical.Encode(value), out)
	}
	if err != nil {
		return Diagnostics{newDiagnostic(StructureInvalidValue, d.at(""), "the document does not match this reader's model of its schema")}
	}
	return nil
}

func decodeStrict(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

// origin is where a declaration sits: its document and the pointer of its declaring member.
type origin struct {
	doc     *document
	pointer string
}

func (o origin) location(sub string) Location {
	if o.doc == nil {
		return NewLocation("", o.pointer+sub, 0, 0)
	}
	return o.doc.at(o.pointer + sub)
}

func (o origin) child(sub string) origin { return origin{doc: o.doc, pointer: o.pointer + sub} }

// reader runs the per-file stages: syntax, compatibility and structure.
type reader struct {
	fsys    fs.FS
	schemas *schemacheck.Set
}

func newReader(fsys fs.FS) (*reader, Diagnostics) {
	set, err := schemacheck.CompileFS(schemas.Files(), schemaDir)
	if err != nil {
		return nil, Diagnostics{newDiagnostic(StructureInvalidSchema, NewLocation("", "", 0, 0), "the embedded schemas do not compile")}
	}
	return &reader{fsys: fsys, schemas: set}, nil
}

var errFileTooLarge = errors.New("file exceeds the size limit")

func readLimited(fsys fs.FS, name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, errFileTooLarge
	}
	return data, nil
}

// read runs the syntax stage on one file. referencedAt is where another document names it; a file
// that cannot be read is reported there.
func (r *reader) read(name string, referencedAt optional[Location]) (*document, Diagnostics) {
	data, err := readLimited(r.fsys, name)
	if errors.Is(err, errFileTooLarge) {
		return nil, Diagnostics{newDiagnostic(SyntaxLimitExceeded, NewLocation(name, "", 0, 0),
			fmt.Sprintf("the file exceeds %d bytes", maxFileBytes))}
	}
	if err != nil {
		at, ok := referencedAt.get()
		if !ok {
			at = NewLocation(name, "", 0, 0)
		}
		return nil, Diagnostics{newDiagnostic(ReferencePathNotFound, at, fmt.Sprintf("file %q is not a readable file", name))}
	}
	return decodeDocument(name, data)
}

func decodeDocument(name string, data []byte) (*document, Diagnostics) {
	tree, positions, violations := yamlvalue.Decode(data, parserLimits())
	if len(violations) == 0 {
		return &document{file: name, tree: tree, positions: positions}, nil
	}
	diagnostics := make(Diagnostics, 0, len(violations))
	for _, violation := range violations {
		code, message := syntaxCode(violation.Class)
		at := NewLocation(name, violation.Pointer, violation.At.Line, violation.At.Column)
		var related []Location
		if violation.Related.Line > 0 {
			related = append(related, NewLocation(name, violation.Pointer, violation.Related.Line, violation.Related.Column))
		}
		diagnostics = append(diagnostics, newDiagnostic(code, at, message, related...))
	}
	return nil, diagnostics
}

func syntaxCode(class yamlvalue.Class) (Code, string) {
	switch class {
	case yamlvalue.InvalidYAML:
		return SyntaxInvalidYAML, "the file is not valid YAML here"
	case yamlvalue.MultipleDocuments:
		return SyntaxMultipleDocuments, "a file holds exactly one YAML document"
	case yamlvalue.DuplicateKey:
		return SyntaxDuplicateKey, "this key appears twice in one mapping"
	case yamlvalue.NonStringKey:
		return SyntaxNonStringKey, "mapping keys must be strings"
	case yamlvalue.Alias:
		return SyntaxAlias, "YAML aliases are not supported; write the value out"
	case yamlvalue.MergeKey:
		return SyntaxMergeKey, "YAML merge keys are not supported; write the members out"
	case yamlvalue.CustomTag:
		return SyntaxCustomTag, "only the core YAML tags str, bool, int, float and timestamp are supported"
	case yamlvalue.NonJSONNumber:
		return SyntaxNonJSONNumber, "numbers must use JSON number syntax within the double-precision range"
	case yamlvalue.NullValue:
		return SyntaxNullValue, "null values are not supported; omit the member instead"
	case yamlvalue.InvalidEncoding:
		return SyntaxInvalidEncoding, "files must be UTF-8 encoded"
	case yamlvalue.FileTooLarge:
		return SyntaxLimitExceeded, fmt.Sprintf("the file exceeds %d bytes", maxFileBytes)
	case yamlvalue.TooDeep:
		return SyntaxLimitExceeded, fmt.Sprintf("mappings and sequences nest deeper than %d levels here", maxDepth)
	case yamlvalue.KeyTooLong:
		return SyntaxLimitExceeded, fmt.Sprintf("a mapping key here is longer than %d bytes", maxKeyBytes)
	case yamlvalue.TooManyValues:
		return SyntaxLimitExceeded, fmt.Sprintf("the file holds more than %d values", maxValues)
	case yamlvalue.TooManyViolations:
		return SyntaxLimitExceeded, truncationMessage()
	default:
		return SyntaxInvalidYAML, "the file is not valid YAML here"
	}
}

// compatibility reads apiVersion and kind before anything else; a document this reader cannot
// read is refused with nothing else checked, so an older reader never acts on part of it.
func compatibility(doc *document) Diagnostics {
	object, isObject := doc.tree.(map[string]any)
	if !isObject {
		return Diagnostics{newDiagnostic(StructureInvalidType, doc.at(""), "a document must be a mapping")}
	}
	apiVersion, present := object["apiVersion"]
	if !present {
		return Diagnostics{newDiagnostic(StructureMissingField, doc.at("/apiVersion"), `required field "apiVersion" is missing`)}
	}
	if text, isText := apiVersion.(string); !isText || !slices.Contains(SupportedAPIVersions(), text) {
		return Diagnostics{newDiagnostic(CompatibilityUnsupportedAPIVersion, doc.at("/apiVersion"),
			fmt.Sprintf("this apiVersion is not supported; supported: %s", strings.Join(SupportedAPIVersions(), ", ")))}
	}
	kind, present := object["kind"]
	if !present {
		return Diagnostics{newDiagnostic(StructureMissingField, doc.at("/kind"), `required field "kind" is missing`)}
	}
	text, isText := kind.(string)
	if _, known := schemaStems()[text]; !isText || !known {
		return Diagnostics{newDiagnostic(CompatibilityUnsupportedKind, doc.at("/kind"),
			fmt.Sprintf("this kind is not supported; supported: %s", strings.Join(knownKinds(), ", ")))}
	}
	doc.kind = text
	return nil
}

// structure checks that the document is one of the expected kinds and valid against its schema.
func (r *reader) structure(doc *document, expected ...string) Diagnostics {
	if !slices.Contains(expected, doc.kind) {
		return Diagnostics{newDiagnostic(StructureUnexpectedKind, doc.at("/kind"),
			fmt.Sprintf("expected a %s document here, not a %s", strings.Join(expected, " or "), doc.kind))}
	}
	return checkSchema(r.schemas, schemaPrefix+schemaStems()[doc.kind], doc.tree, origin{doc: doc})
}

// checkSchema validates tree against schema id and reports each violation at at plus its pointer.
func checkSchema(set *schemacheck.Set, id string, tree any, at origin) Diagnostics {
	result, err := set.Check(id, tree, maxFileDiagnostics)
	if err != nil {
		return Diagnostics{newDiagnostic(StructureInvalidSchema, at.location(""), "the schema for this document is not available")}
	}
	return violationDiagnostics(result, at)
}

// truncationMessage ends a report cut at maxFileDiagnostics. Its code is syntax.limit_exceeded
// because the cap is a parser limit like the others; the message names which one.
func truncationMessage() string {
	return fmt.Sprintf("only the first %d problems found here are reported", maxFileDiagnostics)
}

// violationDiagnostics reports each violation at at plus its pointer, then one diagnostic at at
// when the check stopped at its limit.
func violationDiagnostics(result schemacheck.Result, at origin) Diagnostics {
	var diagnostics Diagnostics
	for _, violation := range result.Violations {
		diagnostics = append(diagnostics, violationDiagnostic(violation, at.location(violation.Pointer)))
	}
	if result.Truncated {
		diagnostics = append(diagnostics, newDiagnostic(SyntaxLimitExceeded, at.location(""), truncationMessage()))
	}
	return diagnostics
}

// violationDiagnostic builds a structure diagnostic from the violation's kind and expected
// constraint alone, never from the validator's own message, which quotes the checked value.
func violationDiagnostic(violation schemacheck.Violation, at Location) Diagnostic {
	where := displayPointer(at.pointer)
	switch violation.Kind {
	case schemacheck.UnknownField:
		return newDiagnostic(StructureUnknownField, at, fmt.Sprintf("%s: field %q is not allowed here", where, violation.Field))
	case schemacheck.MissingField:
		return newDiagnostic(StructureMissingField, at, fmt.Sprintf("%s: required field %q is missing", where, violation.Field))
	case schemacheck.InvalidType:
		return newDiagnostic(StructureInvalidType, at, fmt.Sprintf("%s: expected %s", where, violation.Expected))
	case schemacheck.ExactlyOne:
		return newDiagnostic(StructureExactlyOne, at, fmt.Sprintf("%s: expected %s", where, violation.Expected))
	default:
		return newDiagnostic(StructureInvalidValue, at, fmt.Sprintf("%s: expected %s", where, violation.Expected))
	}
}

func displayPointer(pointer string) string {
	if pointer == "" {
		return "/"
	}
	return pointer
}

// loadSingle runs syntax, compatibility and structure on one standalone document.
func (r *reader) loadSingle(name string, kind string) (*document, Diagnostics) {
	doc, diagnostics := r.read(name, optional[Location]{})
	if len(diagnostics) > 0 {
		return nil, diagnostics.Sorted()
	}
	if diagnostics := compatibility(doc); len(diagnostics) > 0 {
		return nil, diagnostics
	}
	if diagnostics := r.structure(doc, kind); len(diagnostics) > 0 {
		return nil, diagnostics.Sorted()
	}
	return doc, nil
}

func joinPointer(pointer, token string) string { return yamlvalue.Join(pointer, token) }

func compareText(a, b string) int { return canonical.CompareUTF16(a, b) }

func sortedKeys[V any](m map[string]V) []string {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, compareText)
	return keys
}

// countValues counts the values of a decoded tree as the parser's value limit does: every object,
// array and scalar, keys not included.
func countValues(tree any) int {
	count := 1
	switch node := tree.(type) {
	case map[string]any:
		for _, child := range node {
			count += countValues(child)
		}
	case []any:
		for _, child := range node {
			count += countValues(child)
		}
	}
	return count
}

// treeOf converts a canonical value back to the decoded tree form the schema checker reads.
func treeOf(value canonical.Value) any {
	switch node := value.(type) {
	case canonical.Object:
		object := make(map[string]any, len(node))
		for key, child := range node {
			object[key] = treeOf(child)
		}
		return object
	case canonical.Array:
		array := make([]any, len(node))
		for index, child := range node {
			array[index] = treeOf(child)
		}
		return array
	case canonical.String:
		return string(node)
	case canonical.Bool:
		return bool(node)
	case canonical.Number:
		return json.Number(node)
	default:
		return nil
	}
}

// canonicalBytes returns the canonical form of a JSON object read from a schema-valid document.
func canonicalBytes(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return canonical.Encode(canonical.Object{}), nil
	}
	value, err := canonical.Parse(raw)
	if err != nil {
		return nil, err
	}
	return canonical.Encode(value), nil
}
