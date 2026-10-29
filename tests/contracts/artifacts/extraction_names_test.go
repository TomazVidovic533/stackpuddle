package artifacts_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	api "github.com/TomazVidovic533/stackpuddle/artifacts"
	testkit "github.com/TomazVidovic533/stackpuddle/tests/internal/testkit/artifacts"
)

// secondHeaderOffset is where the second entry's header starts when the first entry holds at
// most one block of content.
const secondHeaderOffset = 1024

func TestInspectRejectsNamesUnsafeToExtract(t *testing.T) {
	descriptor := testkit.Entry{Name: "module.yaml", Data: "m"}
	for _, test := range []struct {
		name    string
		entries []testkit.Entry
		code    api.Code
		entry   string
	}{
		{"names equal under case folding", []testkit.Entry{descriptor, {Name: "content/A", Data: "a"}, {Name: "content/a", Data: "a"}}, api.CaseCollision, "content/a"},
		{"Windows device name", []testkit.Entry{descriptor, {Name: "content/CON", Data: "a"}}, api.ReservedName, "content/CON"},
		{"Windows device name with extension", []testkit.Entry{descriptor, {Name: "content/aux.yaml", Data: "a"}}, api.ReservedName, "content/aux.yaml"},
		{"NTFS stream syntax", []testkit.Entry{descriptor, {Name: "content/f:stream", Data: "a"}}, api.StreamName, "content/f:stream"},
		{"escape byte", []testkit.Entry{descriptor, {Name: "content/a\x1bb", Data: "a"}}, api.ControlCharacter, `content/a\x1bb`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := api.Inspect(api.Module, bytes.NewReader(testkit.Archive(t, test.entries...)), api.DefaultLimits())
			requirePublicFailure(t, err, api.InvalidEnvelope, test.code, test.entry)
		})
	}
}

// USTAR names are ASCII when written, so these names are patched into a raw header.
func TestInspectRejectsNonASCIINamesUnsafeToExtract(t *testing.T) {
	for _, test := range []struct {
		name  string
		bytes string
		code  api.Code
		entry string
	}{
		{"C1 control", "\u0085", api.ControlCharacter, `content/\u0085`},
		{"invalid UTF-8", "\xff\xfe", api.UnsafePath, `content/\xff\xfe`},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := testkit.Archive(t, testkit.Entry{Name: "module.yaml", Data: "m"}, testkit.Entry{Name: "content/xx", Data: "a"})
			tarBytes := testkit.Decompress(t, archive)
			copy(tarBytes[secondHeaderOffset+len("content/"):], test.bytes)
			testkit.RepairChecksum(t, tarBytes[secondHeaderOffset:secondHeaderOffset+512])
			err := api.Inspect(api.Module, bytes.NewReader(testkit.Gzip(t, tarBytes)), api.DefaultLimits())
			requirePublicFailure(t, err, api.InvalidEnvelope, test.code, test.entry)
		})
	}
}

func TestBuildRejectsSourceNamesUnsafeToExtract(t *testing.T) {
	for _, test := range []struct {
		name  string
		file  string
		code  api.Code
		entry string
	}{
		{"Windows device name", "CON", api.ReservedName, "content/CON"},
		{"NTFS stream syntax", "f:stream", api.StreamName, "content/f:stream"},
		{"escape byte", "a\x1bb", api.ControlCharacter, `content/a\x1bb`},
		{"C1 control", "a\u0085b", api.ControlCharacter, `content/a\u0085b`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testkit.ModuleSource(t)
			if err := os.WriteFile(filepath.Join(root, "content", test.file), []byte("a"), 0644); err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			_, err := api.Build(api.Module, root, &archive)
			requirePublicFailure(t, err, api.InvalidArgument, test.code, test.entry)
		})
	}
}

func TestBuildRejectsSourceNamesEqualUnderCaseFolding(t *testing.T) {
	root := testkit.ModuleSource(t)
	for _, name := range []string{"Readme", "README"} {
		if err := os.WriteFile(filepath.Join(root, "content", name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(root, "content")); err != nil {
		t.Fatal(err)
	} else if len(entries) != 3 {
		t.Skipf("the filesystem under %s folds case, so a source tree cannot hold two names that differ only by case", root)
	}
	var archive bytes.Buffer
	_, err := api.Build(api.Module, root, &archive)
	requirePublicFailure(t, err, api.InvalidArgument, api.CaseCollision, "content/Readme")
}

func requirePublicFailure(t *testing.T, err error, category api.Category, code api.Code, entry string) {
	t.Helper()
	failure, ok := errors.AsType[*api.Error](err)
	if !ok {
		t.Fatalf("expected %s/%s, got %v", category, code, err)
	}
	if failure.Category != category || failure.Code != code || failure.Entry != entry {
		t.Fatalf("got %s/%s at %q, want %s/%s at %q", failure.Category, failure.Code, failure.Entry, category, code, entry)
	}
}
