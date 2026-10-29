package envelope_test

import (
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/artifacts/envelope"
	"github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

func TestValidateExtractableAcceptsPortableNames(t *testing.T) {
	for _, name := range []string{
		"module.yaml", "content/compose.yaml", "content/.hidden", "content/conf.d/app",
		"content/console.yaml", "content/com", "content/COM10", "content/lptx", "content/Ünïcode.txt",
	} {
		if err := envelope.ValidateExtractable(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestValidateExtractableRejectsEachUnsafeClass(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		code envelope.Code
	}{
		{"invalid UTF-8", "content/\xff\xfe", envelope.UnsafePath},
		{"escape byte", "content/a\x1bb", envelope.ControlCharacter},
		{"tab", "content/a\tb", envelope.ControlCharacter},
		{"delete", "content/a\x7fb", envelope.ControlCharacter},
		{"C1 control", "content/a\u0085b", envelope.ControlCharacter},
		{"NTFS stream", "content/f:stream", envelope.StreamName},
		{"drive letter", "content/c:", envelope.StreamName},
		{"device", "content/CON", envelope.ReservedName},
		{"device in lower case", "content/nul", envelope.ReservedName},
		{"device with extensions", "content/aux.tar.gz", envelope.ReservedName},
		{"device as directory", "content/prn/file", envelope.ReservedName},
		{"serial port zero", "content/COM0", envelope.ReservedName},
		{"parallel port with extension", "content/lpt9.txt", envelope.ReservedName},
		{"superscript serial port", "content/COM¹", envelope.ReservedName},
		{"superscript parallel port", "content/lpt³.log", envelope.ReservedName},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifacts.RequireFailure(t, envelope.ValidateExtractable(test.path), envelope.InvalidEnvelope, test.code)
		})
	}
}

func TestFoldedNamesRejectsNamesEqualUnderCaseFolding(t *testing.T) {
	for _, test := range []struct {
		name          string
		first, second string
	}{
		{"ASCII", "content/A", "content/a"},
		{"directory segment", "content/Dir/x", "content/dir/x"},
		{"Kelvin sign", "content/k", "content/K"},
		{"sharp s", "content/ß", "content/ẞ"},
	} {
		t.Run(test.name, func(t *testing.T) {
			names := envelope.NewFoldedNames()
			if err := names.Claim(test.first); err != nil {
				t.Fatal(err)
			}
			err := names.Claim(test.second)
			artifacts.RequireFailure(t, err, envelope.InvalidEnvelope, envelope.CaseCollision)
			if failure := requireError(t, err); failure.Entry != test.second {
				t.Fatalf("collision names %q, want the later entry %q", failure.Entry, test.second)
			}
		})
	}
}

func TestFoldedNamesLeavesExactRepeatsAndDistinctNames(t *testing.T) {
	names := envelope.NewFoldedNames()
	for _, name := range []string{"content/a", "content/a", "content/b", "content/ab"} {
		if err := names.Claim(name); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
}
