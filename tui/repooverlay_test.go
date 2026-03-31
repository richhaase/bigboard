package tui

import (
	"strings"
	"testing"
)

func TestRenderRepoOverlayContent(t *testing.T) {
	m := Model{
		repoNames:       []string{"repo-a", "repo-b", "repo-c"},
		overlayExcluded: map[string]bool{"repo-b": true},
		overlayCursor:   0,
		viewMode:        ViewRepoOverlay,
		width:           80,
	}

	result := m.renderRepoOverlay()

	// Should contain the title
	if !strings.Contains(result, "REPOSITORIES") {
		t.Error("expected REPOSITORIES title in overlay")
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
	for _, name := range m.repoNames {
		if !strings.Contains(result, name) {
			t.Errorf("expected repo name %q in overlay", name)
		}
	}

	// Should contain help text
	if !strings.Contains(result, "space") || !strings.Contains(result, "toggle") {
		t.Error("expected help text mentioning space/toggle")
	}
}
