package model_test

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/model/canonical"
)

func TestEncodeSortsKeysByUTF16CodeUnits(t *testing.T) {
	t.Parallel()
	// U+1F600 is the surrogate pair D83D DE00, which sorts before U+E000 in UTF-16 although its code
	// point is larger; JavaScript sorts object keys this way.
	got := string(canonical.Encode(canonical.Object{"": canonical.Bool(true), "\U0001F600": canonical.Bool(false), "a": canonical.Bool(true)}))
	want := "{\n  \"a\": true,\n  \"\U0001F600\": false,\n  \"\": true\n}\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeEscapesExactlyWhatJSONStringifyEscapes(t *testing.T) {
	t.Parallel()
	got := string(canonical.Encode(canonical.String("q\" b\\ \b\f\n\r\t \x00\x1f    <&> é")))
	want := "\"q\\\" b\\\\ \\b\\f\\n\\r\\t \\u0000\\u001f    <&> é\"\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Each expected value is what JavaScript's JSON.stringify(Number(text)) prints.
func TestEncodePrintsNumbersAsECMAScriptDoes(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"0": "0", "-0": "0", "1.0": "1", "1e2": "100", "8080": "8080", "-17": "-17",
		"9007199254740991": "9007199254740991", "1152921504606846976": "1152921504606847000",
		"0.1": "0.1", "1.5e3": "1500", "0.000001": "0.000001", "0.0000001": "1e-7", "1e21": "1e+21",
		"123456789012345678901234": "1.2345678901234569e+23", "2.5e-10": "2.5e-10",
	}
	for text, want := range tests {
		if got := string(canonical.Encode(canonical.Number(text))); got != want+"\n" {
			t.Errorf("%s: got %q, want %q", text, got, want)
		}
	}
}

func TestEncodeLayoutMatchesJSONStringifyWithTwoSpaces(t *testing.T) {
	t.Parallel()
	value := canonical.Object{
		"list":  canonical.Array{canonical.Number("1"), canonical.Object{"k": canonical.String("v")}},
		"empty": canonical.Object{"object": canonical.Object{}, "array": canonical.Array{}},
	}
	want := "{\n  \"empty\": {\n    \"array\": [],\n    \"object\": {}\n  },\n  \"list\": [\n    1,\n    {\n      \"k\": \"v\"\n    }\n  ]\n}\n"
	if got := string(canonical.Encode(value)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeIsIndependentOfInsertionOrder(t *testing.T) {
	t.Parallel()
	keys := []string{"zeta", "alpha", "Beta", "_x", "a-b", "a_b", "é", "10", "9"}
	// A fixed seed keeps the orders reproducible; Go's map iteration adds its own randomness.
	shuffle := rand.New(rand.NewPCG(1, 2))
	var first []byte
	for range 50 {
		order := shuffle.Perm(len(keys))
		object := canonical.Object{}
		for _, index := range order {
			object[keys[index]] = canonical.Array{canonical.String(keys[index])}
		}
		encoded := canonical.Encode(object)
		if first == nil {
			first = encoded
			continue
		}
		if !bytes.Equal(encoded, first) {
			t.Fatalf("encoding changed with insertion order:\n%s\n%s", first, encoded)
		}
	}
}

func TestParseKeepsNumberTextAndRefusesTrailingData(t *testing.T) {
	t.Parallel()
	value, err := canonical.Parse([]byte(`{"n": 1.50, "list": []}`))
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(canonical.Object)
	if !ok || object["n"] != canonical.Number("1.50") {
		t.Fatalf("got %#v", value)
	}
	for _, input := range []string{`{} {}`, `null`, `{"a": null}`, ``} {
		if _, err := canonical.Parse([]byte(input)); err == nil {
			t.Errorf("Parse(%q) accepted input outside the canonical value set", input)
		}
	}
}
