// Package canonical writes the canonical JSON form that every stackpuddle reader and writer shares.
//
// The form is what JavaScript's JSON.stringify(value, null, 2) prints after sorting object keys by
// UTF-16 code units, plus a trailing newline, so an SDK in another language can reproduce it byte
// for byte.
package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Value is one JSON value: Object, Array, String, Bool or Number.
//
//sumtype:decl
type Value interface{ canonicalValue() }

// Object is a JSON object. Keys print in UTF-16 code-unit order.
type Object map[string]Value

// Array is a JSON array. Items print in the given order.
type Array []Value

// String is a JSON string.
type String string

// Bool is a JSON boolean.
type Bool bool

// Number is the text of a JSON number, as read from a document.
type Number string

func (Object) canonicalValue() {}
func (Array) canonicalValue()  {}
func (String) canonicalValue() {}
func (Bool) canonicalValue()   {}
func (Number) canonicalValue() {}

const (
	// firstPrintable is U+0020: JSON requires every code point below it to be escaped.
	firstPrintable = 0x20
	indentUnit     = "  "
	// maxSafeInteger is 2^53-1, the largest integer every IEEE-754 double reader keeps exactly.
	maxSafeInteger = 1<<53 - 1
	// exponentBelow and exponentFrom bound the decimal notation of ECMAScript Number::toString.
	exponentBelow = 1e-6
	exponentFrom  = 1e21
)

// ErrNotJSON reports input that is not one JSON value of the supported kinds.
var ErrNotJSON = errors.New("not a JSON value")

// Encode returns the canonical bytes of v, ending in a newline.
func Encode(v Value) []byte {
	var out bytes.Buffer
	write(&out, v, 0)
	out.WriteByte('\n')
	return out.Bytes()
}

// Parse reads exactly one JSON value; numbers keep their text. It refuses null and trailing data.
func Parse(data []byte) (Value, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotJSON, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing data", ErrNotJSON)
	}
	return FromTree(tree)
}

// FromTree converts a decoded tree (map[string]any, []any, string, bool, json.Number) to a Value.
func FromTree(tree any) (Value, error) {
	switch node := tree.(type) {
	case map[string]any:
		object := make(Object, len(node))
		for key, child := range node {
			value, err := FromTree(child)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		return object, nil
	case []any:
		array := make(Array, len(node))
		for index, child := range node {
			value, err := FromTree(child)
			if err != nil {
				return nil, err
			}
			array[index] = value
		}
		return array, nil
	case string:
		return String(node), nil
	case bool:
		return Bool(node), nil
	case json.Number:
		return Number(node), nil
	default:
		return nil, fmt.Errorf("%w: unsupported %T", ErrNotJSON, tree)
	}
}

func write(out *bytes.Buffer, v Value, depth int) {
	switch node := v.(type) {
	case Object:
		writeObject(out, node, depth)
	case Array:
		writeArray(out, node, depth)
	case String:
		writeString(out, string(node))
	case Bool:
		out.WriteString(strconv.FormatBool(bool(node)))
	case Number:
		out.WriteString(formatNumber(string(node)))
	}
}

func writeObject(out *bytes.Buffer, object Object, depth int) {
	if len(object) == 0 {
		out.WriteString("{}")
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, CompareUTF16)
	out.WriteString("{\n")
	for index, key := range keys {
		out.WriteString(strings.Repeat(indentUnit, depth+1))
		writeString(out, key)
		out.WriteString(": ")
		write(out, object[key], depth+1)
		if index < len(keys)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString(strings.Repeat(indentUnit, depth))
	out.WriteByte('}')
}

func writeArray(out *bytes.Buffer, array Array, depth int) {
	if len(array) == 0 {
		out.WriteString("[]")
		return
	}
	out.WriteString("[\n")
	for index, item := range array {
		out.WriteString(strings.Repeat(indentUnit, depth+1))
		write(out, item, depth+1)
		if index < len(array)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString(strings.Repeat(indentUnit, depth))
	out.WriteByte(']')
}

// writeString escapes exactly what JSON.stringify escapes: the quote, the backslash and U+0000 to
// U+001F. Unlike encoding/json it leaves <, >, & and U+2028/U+2029 as they are.
func writeString(out *bytes.Buffer, text string) {
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < firstPrintable {
				out.WriteString(fmt.Sprintf(`\u%04x`, r))
				continue
			}
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
}

// formatNumber prints a JSON number as ECMAScript Number::toString prints its double value.
func formatNumber(text string) string {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Readers admit only numbers within the double range, so this text is already canonical.
		return text
	}
	if value == math.Trunc(value) && math.Abs(value) <= maxSafeInteger {
		return strconv.FormatInt(int64(value), 10)
	}
	format := byte('f')
	if magnitude := math.Abs(value); magnitude < exponentBelow || magnitude >= exponentFrom {
		format = 'e'
	}
	formatted := strconv.FormatFloat(value, format, -1, 64)
	if format == 'e' {
		// ECMAScript writes the exponent without leading zeros: 1e-7, not 1e-07.
		mantissa, exponent, _ := strings.Cut(formatted, "e")
		sign, digits := exponent[:1], strings.TrimLeft(exponent[1:], "0")
		formatted = mantissa + "e" + sign + digits
	}
	return formatted
}

// CompareUTF16 orders strings by their UTF-16 code units, the order JavaScript sorts keys in.
func CompareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
