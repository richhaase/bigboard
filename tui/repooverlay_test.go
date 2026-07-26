package tui

import (
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestRenderRepoOverlayContent(t *testing.T) {
	m := Model{
		loadedRepos: []git.Repository{
			{ID: "repo-a", Name: "repo-a"},
			{ID: "repo-b", Name: "repo-b"},
			{ID: "repo-c", Name: "repo-c"},
		},
		overlayExcluded: map[string]bool{"repo-b": true},
		overlayCursor:   0,
		viewMode:        ViewRepoOverlay,
		width:           80,
	}

	result := m.renderRepoOverlay()

	// Should contain the title
	if !strings.Contains(result, "REPOSITORY") {
		t.Error("expected REPOSITORY title in overlay")
	}

	// Should show repo-a as included (checkbox checked)
	if !strings.Contains(result, "[x]") {
		t.Error("expected [x] checkbox for included repo")
	}

	// Should show repo-b as excluded (checkbox unchecked)
	if !strings.Contains(result, "[ ]") {
		t.Error("expected [ ] checkbox for excluded repo")
	}

	// Should contain all repo names
	for _, repo := range m.loadedRepos {
		if !strings.Contains(result, repo.Name) {
			t.Errorf("expected repo name %q in overlay", repo.Name)
		}
	}

	// Should contain help text
	if !strings.Contains(result, "space") || !strings.Contains(result, "toggle") {
		t.Error("expected help text mentioning space/toggle")
	}
}
