package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestGlanceScopedDuplicateNames(t *testing.T) {
	m := monorepoFixture()
	base := m.allRecords[0]
	m.allRecords = nil
	for _, email := range []string{"alice@example.com", "bob@example.com"} {
		r := base
		r.CommitID = email
		r.Email = email
		r.Author = "Same area-local display name"
		r.Changes = []git.PathChange{{Path: "feature/file.go"}}
		m.allRecords = append(m.allRecords, r)
		for i := 0; i < 3; i++ {
			r.CommitID = fmt.Sprintf("%s-%d", email, i)
			r.Author = email
			r.Changes = []git.PathChange{{Path: "other/file.go"}}
			m.allRecords = append(m.allRecords, r)
		}
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:feature"
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "2")
	out := m.View()
	if !strings.Contains(out, "alice@example.com") || !strings.Contains(out, "bob@example.com") {
		t.Fatalf("scoped duplicate identities hidden:\n%s", out)
	}
	m = pressAwareness(m, "/")
	m = pressAwareness(m, "alice@example.com")
	m = pressAwareness(m, "enter")
	if !strings.Contains(m.View(), "alice@example.com") {
		t.Fatal("search hid distinction from unfiltered area identities")
	}
}
