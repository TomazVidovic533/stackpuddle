//go:build acceptance

package harness

import "testing"

func TestOwnsMatchesOnlyThisStacksProjects(t *testing.T) {
	t.Parallel()
	s := &Stack{Name: "sp-m01-070-abc1234-wxyz"}
	for project, want := range map[string]bool{
		"sp-m01-070-abc1234-wxyz":           true,
		"sp-m01-070-abc1234-wxyz-dev-q3k9":  true,
		"other-sp-m01-070-abc1234-wxyz-dev": false,
		"sp-m01-070-abc1234-wxyzz-dev":      false,
		"sp-m01-070-abc1234-wxy":            false,
		"":                                  false,
	} {
		if got := s.owns(project); got != want {
			t.Errorf("owns(%q) = %v, want %v", project, got, want)
		}
	}
}
