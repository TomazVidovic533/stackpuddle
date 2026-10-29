package contracts_test

import (
	"regexp"
	"testing"
	"testing/fstest"

	"os"
	"path/filepath"

	"github.com/TomazVidovic533/stackpuddle/model"
)

var journeyFence = regexp.MustCompile("(?s)```yaml ([^\\n]+)\\n(.*?)```")

func TestJourneySnippets_EveryProjectSnippetLoadsWithNoDiagnostics(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "journey.md"))
	if err != nil {
		t.Fatal(err)
	}
	snippets := journeyFence.FindAllSubmatch(doc, -1)
	if len(snippets) == 0 {
		t.Fatal("docs/journey.md has no `yaml <file>` snippet to prove")
	}
	definition := loadPostgresDefinition(t)
	for _, s := range snippets {
		name := string(s[1])
		fsys := fstest.MapFS{name: {Data: s[2]}}
		if _, diagnostics := model.Load(fsys, name, []model.ModuleDefinition{definition}); len(diagnostics) > 0 {
			t.Errorf("snippet %s does not load: %v", name, describeModel(diagnostics))
		}
	}
}
