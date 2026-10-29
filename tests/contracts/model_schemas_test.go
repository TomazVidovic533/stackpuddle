package contracts_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/TomazVidovic533/stackpuddle/internal/model/schemacheck"
	"github.com/TomazVidovic533/stackpuddle/schemas"
)

const (
	publishedSchemaDir    = "../../schemas/v1"
	sharedDefinitionsStem = "definitions"
)

// publishedSchemaStems lists the schema files in the repository, by stem.
func publishedSchemaStems(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(publishedSchemaDir, "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	stems := make([]string, 0, len(files))
	for _, file := range files {
		stems = append(stems, strings.TrimSuffix(filepath.Base(file), ".schema.json"))
	}
	return stems
}

func readPublishedSchema(t *testing.T, stem string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(publishedSchemaDir, stem+".schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode %s: %v", stem, err)
	}
	return document
}

func TestModelSchemas_CompileWithLoadingRefused(t *testing.T) {
	t.Parallel()
	for name, fsys := range map[string]fs.FS{"repository": os.DirFS("../../schemas"), "embedded": schemas.Files()} {
		if _, err := schemacheck.CompileFS(fsys, "v1"); err != nil {
			t.Errorf("%s schemas do not compile with loading refused: %v", name, err)
		}
	}
}

// TestModelSchemas_IdentifiersMatchFileAndKind holds ADR-0006: the $id is the stem's URN and a
// document schema's kind is the stem in PascalCase.
func TestModelSchemas_IdentifiersMatchFileAndKind(t *testing.T) {
	t.Parallel()
	for _, stem := range publishedSchemaStems(t) {
		document := readPublishedSchema(t, stem)
		if id := document["$id"]; id != schemaBase+stem {
			t.Errorf("%s: $id %v, want %s", stem, id, schemaBase+stem)
		}
		if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s: not a draft 2020-12 schema", stem)
		}
		if stem == sharedDefinitionsStem {
			continue
		}
		properties, _ := document["properties"].(map[string]any)
		kind, _ := properties["kind"].(map[string]any)
		if kind["const"] != pascalCase(stem) {
			t.Errorf("%s: kind const %v, want %s", stem, kind["const"], pascalCase(stem))
		}
	}
}

func pascalCase(stem string) string {
	var builder strings.Builder
	for _, word := range strings.Split(stem, "-") {
		builder.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return builder.String()
}

func TestModelSchemas_EmbeddedSetEqualsTheDirectory(t *testing.T) {
	t.Parallel()
	embedded, err := fs.Glob(schemas.Files(), "v1/*")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(publishedSchemaDir)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, entry := range entries {
		onDisk = append(onDisk, "v1/"+entry.Name())
	}
	if !slices.Equal(embedded, onDisk) {
		t.Fatalf("embedded %v, directory %v", embedded, onDisk)
	}
	for _, name := range embedded {
		embeddedData, err := fs.ReadFile(schemas.Files(), name)
		if err != nil {
			t.Fatal(err)
		}
		diskData, err := os.ReadFile(filepath.Join(publishedSchemaDir, strings.TrimPrefix(name, "v1/")))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embeddedData, diskData) {
			t.Errorf("%s: embedded bytes differ from the file", name)
		}
	}
}
