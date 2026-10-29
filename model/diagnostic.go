package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

// Stage is the pipeline stage a diagnostic code belongs to. Stages run in declaration order and a
// stage runs only when the stages before it found nothing, so each failure shows its root cause.
type Stage int

const (
	StageSyntax Stage = iota + 1
	StageCompatibility
	StageStructure
	StageIdentity
	StageReference
	StageBinding
	StageGraph
	StageModule
)

// String returns the stage's code prefix, such as "syntax"; module stage codes have no fixed prefix.
func (s Stage) String() string {
	switch s {
	case StageSyntax:
		return "syntax"
	case StageCompatibility:
		return "compatibility"
	case StageStructure:
		return "structure"
	case StageIdentity:
		return "identity"
	case StageReference:
		return "reference"
	case StageBinding:
		return "binding"
	case StageGraph:
		return "graph"
	case StageModule:
		return "module"
	default:
		return "unknown"
	}
}

// Code is a stable diagnostic code, "<stage>.<name>". Match on codes, never on message text.
type Code string

// The core codes. Each is a published name: it is kept or retired explicitly, never renamed.
const (
	SyntaxInvalidYAML                  Code = "syntax.invalid_yaml"
	SyntaxMultipleDocuments            Code = "syntax.multiple_documents"
	SyntaxDuplicateKey                 Code = "syntax.duplicate_key"
	SyntaxNonStringKey                 Code = "syntax.non_string_key"
	SyntaxAlias                        Code = "syntax.alias"
	SyntaxMergeKey                     Code = "syntax.merge_key"
	SyntaxCustomTag                    Code = "syntax.custom_tag"
	SyntaxNonJSONNumber                Code = "syntax.non_json_number"
	SyntaxNullValue                    Code = "syntax.null_value"
	SyntaxInvalidEncoding              Code = "syntax.invalid_encoding"
	SyntaxLimitExceeded                Code = "syntax.limit_exceeded"
	CompatibilityUnsupportedAPIVersion Code = "compatibility.unsupported_api_version"
	CompatibilityUnsupportedKind       Code = "compatibility.unsupported_kind"
	CompatibilityModuleNotInBundle     Code = "compatibility.module_not_in_bundle"
	CompatibilityBundleVersionMismatch Code = "compatibility.bundle_version_mismatch"
	StructureUnknownField              Code = "structure.unknown_field"
	StructureMissingField              Code = "structure.missing_field"
	StructureInvalidType               Code = "structure.invalid_type"
	StructureInvalidValue              Code = "structure.invalid_value"
	StructureExactlyOne                Code = "structure.exactly_one"
	StructureUnexpectedKind            Code = "structure.unexpected_kind"
	StructureSecretLiteral             Code = "structure.secret_literal"
	StructureInvalidSchema             Code = "structure.invalid_schema"
	IdentityDuplicateDeclaration       Code = "identity.duplicate_declaration"
	IdentityDefinitionNameMismatch     Code = "identity.definition_name_mismatch"
	ReferencePathNotFound              Code = "reference.path_not_found"
	ReferenceUnknownObject             Code = "reference.unknown_object"
	ReferenceWrongKind                 Code = "reference.wrong_kind"
	ReferenceUnknownDependency         Code = "reference.unknown_dependency"
	ReferenceUnknownExport             Code = "reference.unknown_export"
	ReferenceUnknownListener           Code = "reference.unknown_listener"
	ReferenceUnknownDefinition         Code = "reference.unknown_definition"
	ReferenceUnknownInput              Code = "reference.unknown_input"
	ReferenceUnknownValue              Code = "reference.unknown_value"
	ReferenceModuleNotInManifest       Code = "reference.module_not_in_manifest"
	ReferenceInvalidRef                Code = "reference.invalid_ref"
	BindingDuplicate                   Code = "binding.duplicate"
	BindingConflict                    Code = "binding.conflict"
	BindingCapabilityMismatch          Code = "binding.capability_mismatch"
	BindingUnboundDependency           Code = "binding.unbound_dependency"
	BindingDeliveryConflict            Code = "binding.delivery_conflict"
	BindingDeliveryUnsupported         Code = "binding.delivery_unsupported"
	BindingUnreachableContext          Code = "binding.unreachable_context"
	BindingAddressUnavailable          Code = "binding.address_unavailable"
	GraphPrerequisiteCycle             Code = "graph.prerequisite_cycle"
)

// Stage returns the stage the code belongs to: the core stage its prefix names, or StageModule for
// a code a module validator reports.
func (c Code) Stage() Stage {
	prefix, _, _ := strings.Cut(string(c), ".")
	for _, stage := range coreStages() {
		if prefix == stage.String() {
			return stage
		}
	}
	return StageModule
}

func coreStages() []Stage {
	return []Stage{StageSyntax, StageCompatibility, StageStructure, StageIdentity, StageReference, StageBinding, StageGraph}
}

// Location is where a diagnostic applies: a file relative to the loaded file system, a JSON pointer
// into that file's document, and the line and column of the member when known.
type Location struct {
	file    string
	pointer string
	line    int
	column  int
}

// NewLocation returns a Location. A line or column below 1 means unknown.
func NewLocation(file, pointer string, line, column int) Location {
	return Location{file: file, pointer: pointer, line: max(line, 0), column: max(column, 0)}
}

// File returns the file path relative to the loaded file system, or "" for bytes with no file.
func (l Location) File() string { return l.file }

// Pointer returns the JSON pointer of the member, "" for the whole document.
func (l Location) Pointer() string { return l.pointer }

// Position returns the 1-based line and column. ok is false when the line is unknown; column is 0
// when only the line is known.
func (l Location) Position() (line, column int, ok bool) {
	return l.line, l.column, l.line > 0
}

// Diagnostic is one finding: a stable code, where it applies, a message and related locations.
// Messages name pointers, identifiers and expected constraints only, never a declared value; they
// are for people and are not part of the contract.
type Diagnostic struct {
	code    Code
	at      Location
	message string
	related []Location
}

// Code returns the diagnostic's stable code.
func (d Diagnostic) Code() Code { return d.code }

// Location returns where the diagnostic applies.
func (d Diagnostic) Location() Location { return d.at }

// At returns a copy of d located at at, with the same code, message and related locations. It lets a
// caller place a diagnostic whose pointer is relative to a document it only holds in pieces, such as
// the options ValidateOptions or a module validator checked, in the file the options came from.
func (d Diagnostic) At(at Location) Diagnostic {
	d.at = at
	d.related = slices.Clone(d.related)
	return d
}

// Message returns the human-readable explanation.
func (d Diagnostic) Message() string { return d.message }

// Related returns other locations involved, such as an earlier duplicate. Each call returns a copy.
func (d Diagnostic) Related() []Location { return slices.Clone(d.related) }

// moduleCode is the shape of a module validator's code: "<module>.<rule>".
var moduleCode = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)

// NewModuleDiagnostic returns a diagnostic for a module validator's own rule. The code must look
// like "<module>.<rule>" and must not use a core stage prefix; otherwise it returns an *Error.
func NewModuleDiagnostic(code Code, at Location, message string) (Diagnostic, error) {
	if !moduleCode.MatchString(string(code)) {
		return Diagnostic{}, invalidValue("module diagnostic code %q must look like <module>.<rule>", string(code))
	}
	if code.Stage() != StageModule {
		return Diagnostic{}, invalidValue("module diagnostic code %q uses a core stage prefix", string(code))
	}
	return Diagnostic{code: code, at: at, message: message}, nil
}

// Diagnostics is a list of findings.
type Diagnostics []Diagnostic

// Sorted returns a copy ordered by file, line, column, pointer and code, comparing strings by UTF-16
// code units, so every reader reports the same order.
func (d Diagnostics) Sorted() Diagnostics {
	sorted := slices.Clone(d)
	slices.SortStableFunc(sorted, compareDiagnostics)
	return sorted
}

func compareDiagnostics(a, b Diagnostic) int {
	if c := canonical.CompareUTF16(a.at.file, b.at.file); c != 0 {
		return c
	}
	if c := compareInts(a.at.line, b.at.line); c != 0 {
		return c
	}
	if c := compareInts(a.at.column, b.at.column); c != 0 {
		return c
	}
	if c := canonical.CompareUTF16(a.at.pointer, b.at.pointer); c != 0 {
		return c
	}
	return canonical.CompareUTF16(string(a.code), string(b.code))
}

// Error reports why a Parse function or NewModuleDiagnostic refused its input. Match it with
// errors.AsType[*model.Error] and branch on Code, never on the message.
type Error struct {
	code    Code
	message string
}

// Error returns the message.
func (e *Error) Error() string { return e.message }

// Code returns the stable code of the refusal.
func (e *Error) Code() Code { return e.code }

func invalidValue(format string, args ...any) error {
	return &Error{code: StructureInvalidValue, message: fmt.Sprintf(format, args...)}
}

func invalidRef(format string, args ...any) error {
	return &Error{code: ReferenceInvalidRef, message: fmt.Sprintf(format, args...)}
}

func newDiagnostic(code Code, at Location, message string, related ...Location) Diagnostic {
	return Diagnostic{code: code, at: at, message: message, related: related}
}
