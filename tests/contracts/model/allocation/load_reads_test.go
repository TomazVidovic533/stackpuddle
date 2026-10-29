package allocation_test

import (
	"fmt"
	"io/fs"
	"maps"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/TomazVidovic533/stackpuddle/model"
)

// definitionDirLimit is how many directories one project loads local definitions from.
const definitionDirLimit = 16

// countingFS counts the opens of each file. It offers Open alone, so every read and every existence
// check Load makes goes through it.
type countingFS struct {
	files fstest.MapFS
	opens map[string]int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opens[name]++
	return c.files.Open(name)
}

func TestLoadReadsEachDefinitionDirectoryOnceAndAtMostTheLimit(t *testing.T) {
	tests := map[string]struct {
		// paths are the directories the definition entries name, in key order.
		paths []string
		want  map[model.Code]int
		// limitAt is the pointer the definition-directory limit is reported at, when it is.
		limitAt string
		// descriptorOpens is how often Load opens each directory's module.yaml.
		descriptorOpens map[string]int
	}{
		"many entries naming one directory": {
			paths:           repeated("lib", sharingDefinitions),
			want:            map[model.Code]int{model.IdentityDefinitionNameMismatch: sharingDefinitions},
			descriptorOpens: map[string]int{"lib/module.yaml": 1},
		},
		"one directory more than the limit": {
			paths:           distinct(definitionDirLimit + 1),
			want:            map[model.Code]int{model.SyntaxLimitExceeded: 1},
			limitAt:         fmt.Sprintf("/spec/definitions/d%05d/path", definitionDirLimit),
			descriptorOpens: opensUpTo(definitionDirLimit+1, definitionDirLimit),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fsys := definitionsProject(test.paths)
			_, diagnostics := model.Load(fsys, rootFile, nil)
			if got := countByCode(diagnostics); !equalCounts(got, test.want) {
				t.Fatalf("diagnostics by code: got %v, want %v", got, test.want)
			}
			for _, diagnostic := range diagnostics {
				if at := diagnostic.Location().Pointer(); diagnostic.Code() == model.SyntaxLimitExceeded && at != test.limitAt {
					t.Fatalf("the limit is reported at %q, want %q", at, test.limitAt)
				}
			}
			got := map[string]int{}
			for file := range fsys.files {
				if strings.HasSuffix(file, "/module.yaml") {
					got[file] = fsys.opens[file]
				}
			}
			if !maps.Equal(got, test.descriptorOpens) {
				t.Fatalf("module.yaml opens: got %v, want %v", got, test.descriptorOpens)
			}
		})
	}
}

// definitionsProject declares one definition entry per path, keyed so key order is path order,
// with a module.yaml in each distinct directory. No entry's name is its module's name.
func definitionsProject(paths []string) *countingFS {
	files := fstest.MapFS{}
	var root strings.Builder
	root.WriteString(projectHeader + "spec:\n  definitions:\n")
	for index, dir := range paths {
		fmt.Fprintf(&root, "    d%05d: {path: %s}\n", index, dir)
		files[dir+"/module.yaml"] = &fstest.MapFile{Data: []byte(
			"apiVersion: stackpuddle/v1\nkind: Module\nname: lib\nversion: 0.1.0\nworkloads:\n  store: {type: service, image: busybox}\n")}
	}
	root.WriteString(environment)
	files[rootFile] = &fstest.MapFile{Data: []byte(root.String())}
	return &countingFS{files: files, opens: map[string]int{}}
}

func repeated(dir string, count int) []string {
	paths := make([]string, count)
	for index := range paths {
		paths[index] = dir
	}
	return paths
}

func distinct(count int) []string {
	paths := make([]string, count)
	for index := range paths {
		paths[index] = fmt.Sprintf("lib%02d", index)
	}
	return paths
}

// opensUpTo expects each of count distinct directories' module.yaml opened once up to read, and
// never after.
func opensUpTo(count, read int) map[string]int {
	opens := map[string]int{}
	for index, dir := range distinct(count) {
		opens[dir+"/module.yaml"] = 0
		if index < read {
			opens[dir+"/module.yaml"] = 1
		}
	}
	return opens
}
