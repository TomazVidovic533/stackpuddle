// Package yamlvalue decodes the strict YAML subset of stackpuddle documents into a JSON-compatible tree.
//
// The subset admits exactly what JSON can represent: one document, string keys, no aliases, merge keys,
// nulls or custom tags, and numbers in JSON number grammar. Anything else is a classified violation
// with the JSON pointer and source position where it occurs.
package yamlvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Class names the syntax rule a document broke.
type Class int

const (
	InvalidYAML Class = iota + 1
	MultipleDocuments
	DuplicateKey
	NonStringKey
	Alias
	MergeKey
	CustomTag
	NonJSONNumber
	NullValue
	InvalidEncoding
	// FileTooLarge, TooDeep, KeyTooLong, TooManyValues and TooManyViolations each name the Limits
	// field the document exceeded.
	FileTooLarge
	TooDeep
	KeyTooLong
	TooManyValues
	TooManyViolations
)

// Limits bound the work one decoded file can cause. Every field must be positive.
type Limits struct {
	// FileBytes caps the size of the input.
	FileBytes int
	// Depth caps how deeply mappings and sequences nest.
	Depth int
	// KeyBytes caps one mapping key. YAML caps only implicit keys, so an explicit "? key" needs it.
	KeyBytes int
	// Values caps the values in the file: every mapping, sequence and scalar that is not a key.
	Values int
	// Violations caps the failures reported; one TooManyViolations then says the rest were dropped.
	Violations int
}

// Position is a 1-based line and column counted in Unicode code points. Zero means unknown.
type Position struct {
	Line   int
	Column int
}

// Span holds where a member's key and its value start. For array items both are the item.
type Span struct {
	Key   Position
	Value Position
}

// Positions records where each member of a decoded tree starts, in a tree shaped like the value. It
// stores no JSON pointer, since a lookup walks the tree token by token, so its size grows with the
// number of values, never with key length times depth. The zero value holds no positions. It is
// safe for concurrent reads.
type Positions struct {
	root *positionNode
}

type positionNode struct {
	span    Span
	members map[string]*positionNode
	items   []positionNode
}

// Violation is one syntax failure. Related is the earlier key of a duplicate; zero otherwise.
type Violation struct {
	Class   Class
	Pointer string
	At      Position
	Related Position
}

// Value is the decoded tree: map[string]any, []any, string, bool or json.Number.
type Value = any

const (
	tagString    = "!!str"
	tagBool      = "!!bool"
	tagInt       = "!!int"
	tagFloat     = "!!float"
	tagTimestamp = "!!timestamp"
	tagNull      = "!!null"
	tagMerge     = "!!merge"
	tagMap       = "!!map"
	tagSeq       = "!!seq"
)

// jsonNumber is the JSON number grammar (RFC 8259 section 6).
var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

var yamlErrorLine = regexp.MustCompile(`line ([0-9]+)`)

// Decode parses data into a tree. It returns a nil tree when any violation exists; the violations
// then name every independent failure found, in document order, up to limits.Violations. A broken
// Values or Violations limit ends the walk. Decode never panics on any input.
func Decode(data []byte, limits Limits) (Value, Positions, []Violation) {
	if len(data) > limits.FileBytes {
		return nil, Positions{}, []Violation{{Class: FileTooLarge}}
	}
	if !utf8.Valid(data) {
		return nil, Positions{}, []Violation{{Class: InvalidEncoding, At: firstInvalidByte(data)}}
	}
	root, violation, ok := parseSingleDocument(data)
	if !ok {
		return nil, Positions{}, []Violation{violation}
	}
	walker := treeWalker{limits: limits}
	positions := Positions{root: &positionNode{span: Span{Key: positionOf(root), Value: positionOf(root)}}}
	var value Value
	if walker.admit(1, root) {
		value = walker.value(root, positions.root, 1)
	}
	if len(walker.violations) > 0 {
		return nil, Positions{}, walker.violations
	}
	return value, positions, nil
}

// parseSingleDocument runs the YAML parser, which reports its own failures as errors and, on some
// malformed inputs, as panics; both become InvalidYAML so untrusted bytes cannot crash a caller.
func parseSingleDocument(data []byte) (root *yaml.Node, violation Violation, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			root, violation, ok = nil, Violation{Class: InvalidYAML}, false
		}
	}()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, Violation{Class: InvalidYAML, At: errorPosition(err)}, false
	}
	var extra yaml.Node
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return nil, Violation{Class: MultipleDocuments, At: Position{Line: extra.Line, Column: extra.Column}}, false
	case !errors.Is(err, io.EOF):
		return nil, Violation{Class: InvalidYAML, At: errorPosition(err)}, false
	}
	if len(document.Content) != 1 {
		return nil, Violation{Class: InvalidYAML}, false
	}
	return document.Content[0], Violation{}, true
}

func errorPosition(err error) Position {
	match := yamlErrorLine.FindStringSubmatch(err.Error())
	if match == nil {
		return Position{}
	}
	line, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return Position{}
	}
	return Position{Line: line}
}

func firstInvalidByte(data []byte) Position {
	line, column := 1, 1
	for offset := 0; offset < len(data); {
		r, size := utf8.DecodeRune(data[offset:])
		if r == utf8.RuneError && size <= 1 {
			return Position{Line: line, Column: column}
		}
		if r == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
		offset += size
	}
	return Position{}
}

// treeWalker converts the parsed nodes. path holds the reference tokens from the root to the node
// being walked, so a pointer is joined only for a violation, never once per node.
type treeWalker struct {
	limits     Limits
	path       []string
	values     int
	violations []Violation
	stopped    bool
}

func (w *treeWalker) pointer() string { return Pointer(w.path) }

// fail records class at node under the path being walked. Once limits.Violations are recorded,
// it records TooManyViolations instead and ends the walk.
func (w *treeWalker) fail(class Class, node *yaml.Node) {
	w.record(class, node, Position{})
}

// failMember records class at the member key names, with related as the earlier key, if any.
func (w *treeWalker) failMember(class Class, key *yaml.Node, related Position) {
	w.path = append(w.path, key.Value)
	w.record(class, key, related)
	w.path = w.path[:len(w.path)-1]
}

func (w *treeWalker) record(class Class, node *yaml.Node, related Position) {
	if w.stopped {
		return
	}
	if len(w.violations) >= w.limits.Violations {
		w.violations = append(w.violations, Violation{Class: TooManyViolations})
		w.stopped = true
		return
	}
	w.violations = append(w.violations, Violation{Class: class, Pointer: w.pointer(), At: positionOf(node), Related: related})
}

// admit counts count more values below node, and ends the walk at node once they exceed
// limits.Values. A collection is admitted before its members are allocated.
func (w *treeWalker) admit(count int, node *yaml.Node) bool {
	w.values += count
	if w.values <= w.limits.Values {
		return true
	}
	w.violations = append(w.violations, Violation{Class: TooManyValues, Pointer: w.pointer(), At: positionOf(node)})
	w.stopped = true
	return false
}

func (w *treeWalker) value(node *yaml.Node, at *positionNode, depth int) Value {
	if node.Kind == yaml.AliasNode || node.Alias != nil {
		w.fail(Alias, node)
		return nil
	}
	switch node.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		if depth > w.limits.Depth {
			w.fail(TooDeep, node)
			return nil
		}
		if node.Kind == yaml.MappingNode {
			return w.mapping(node, at, depth)
		}
		return w.sequence(node, at, depth)
	case yaml.ScalarNode:
		return w.scalar(node)
	default:
		w.fail(InvalidYAML, node)
		return nil
	}
}

func (w *treeWalker) mapping(node *yaml.Node, at *positionNode, depth int) Value {
	if node.ShortTag() != tagMap {
		w.fail(CustomTag, node)
		return nil
	}
	if !w.admit(len(node.Content)/2, node) {
		return nil
	}
	result := make(map[string]any, len(node.Content)/2)
	at.members = make(map[string]*positionNode, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content) && !w.stopped; index += 2 {
		key, child := node.Content[index], node.Content[index+1]
		if !w.acceptKey(key, at.members) {
			continue
		}
		member := &positionNode{span: Span{Key: positionOf(key), Value: positionOf(child)}}
		at.members[key.Value] = member
		w.path = append(w.path, key.Value)
		result[key.Value] = w.value(child, member, depth+1)
		w.path = w.path[:len(w.path)-1]
	}
	return result
}

// acceptKey reports whether key may name a member of a mapping that already holds members,
// recording the violation when it may not.
func (w *treeWalker) acceptKey(key *yaml.Node, members map[string]*positionNode) bool {
	if key.Kind == yaml.ScalarNode && key.ShortTag() == tagMerge {
		w.failMember(MergeKey, key, Position{})
		return false
	}
	if key.Kind != yaml.ScalarNode || key.Alias != nil || key.ShortTag() != tagString {
		w.fail(NonStringKey, key)
		return false
	}
	if len(key.Value) > w.limits.KeyBytes {
		// Reported at the mapping, so the pointer never carries the oversized key.
		w.fail(KeyTooLong, key)
		return false
	}
	if !utf8.ValidString(key.Value) {
		w.failMember(InvalidEncoding, key, Position{})
		return false
	}
	if earlier, exists := members[key.Value]; exists {
		w.failMember(DuplicateKey, key, earlier.span.Key)
		return false
	}
	return true
}

func (w *treeWalker) sequence(node *yaml.Node, at *positionNode, depth int) Value {
	if node.ShortTag() != tagSeq {
		w.fail(CustomTag, node)
		return nil
	}
	if !w.admit(len(node.Content), node) {
		return nil
	}
	result := make([]any, len(node.Content))
	at.items = make([]positionNode, len(node.Content))
	for index, child := range node.Content {
		if w.stopped {
			break
		}
		item := &at.items[index]
		item.span = Span{Key: positionOf(child), Value: positionOf(child)}
		w.path = append(w.path, strconv.Itoa(index))
		result[index] = w.value(child, item, depth+1)
		w.path = w.path[:len(w.path)-1]
	}
	return result
}

func (w *treeWalker) scalar(node *yaml.Node) Value {
	switch node.ShortTag() {
	case tagString, tagTimestamp:
		if !utf8.ValidString(node.Value) {
			w.fail(InvalidEncoding, node)
			return nil
		}
		return node.Value
	case tagBool:
		return w.boolean(node)
	case tagInt, tagFloat:
		return w.number(node)
	case tagNull:
		w.fail(NullValue, node)
		return nil
	default:
		w.fail(CustomTag, node)
		return nil
	}
}

func (w *treeWalker) boolean(node *yaml.Node) Value {
	switch strings.ToLower(node.Value) {
	case "true":
		return true
	case "false":
		return false
	default:
		w.fail(InvalidYAML, node)
		return nil
	}
}

func (w *treeWalker) number(node *yaml.Node) Value {
	if !jsonNumber.MatchString(node.Value) {
		w.fail(NonJSONNumber, node)
		return nil
	}
	if _, err := strconv.ParseFloat(node.Value, 64); err != nil {
		// Out of the float64 range: JSON readers in other languages could not represent it.
		w.fail(NonJSONNumber, node)
		return nil
	}
	return json.Number(node.Value)
}

func positionOf(node *yaml.Node) Position {
	return Position{Line: node.Line, Column: node.Column}
}

// Join appends one reference token to a JSON pointer, escaping "~" and "/" (RFC 6901).
func Join(pointer, token string) string {
	return pointer + "/" + escapeToken(token)
}

// Pointer joins reference tokens from the root into one JSON pointer, in time and memory linear in
// its length, where joining them one at a time copies every prefix again.
func Pointer(tokens []string) string {
	var builder strings.Builder
	size := len(tokens)
	for _, token := range tokens {
		size += len(token)
	}
	builder.Grow(size)
	for _, token := range tokens {
		builder.WriteByte('/')
		builder.WriteString(escapeToken(token))
	}
	return builder.String()
}

func escapeToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// Span returns the span of the member at pointer, if the tree holds that member.
func (p Positions) Span(pointer string) (Span, bool) {
	node, complete := p.nearest(pointer)
	if node == nil || !complete {
		return Span{}, false
	}
	return node.span, true
}

// Locate returns the position of the member at pointer, or of its nearest existing ancestor when the
// member is absent, so a missing field points at the object that lacks it.
func (p Positions) Locate(pointer string) Position {
	node, _ := p.nearest(pointer)
	if node == nil {
		return Position{}
	}
	return node.span.Key
}

// nearest returns the deepest recorded node on pointer's path and whether it is the node pointer
// names.
func (p Positions) nearest(pointer string) (*positionNode, bool) {
	node := p.root
	if node == nil || pointer == "" {
		return node, node != nil
	}
	if pointer[0] != '/' {
		return node, false
	}
	for token := range strings.SplitSeq(pointer[1:], "/") {
		child, ok := node.child(unescapeToken(token))
		if !ok {
			return node, false
		}
		node = child
	}
	return node, true
}

func (n *positionNode) child(token string) (*positionNode, bool) {
	if n.members != nil {
		member, ok := n.members[token]
		return member, ok
	}
	index, err := strconv.Atoi(token)
	if err != nil || index < 0 || index >= len(n.items) || strconv.Itoa(index) != token {
		return nil, false
	}
	return &n.items[index], true
}

func unescapeToken(token string) string {
	if !strings.Contains(token, "~") {
		return token
	}
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}
