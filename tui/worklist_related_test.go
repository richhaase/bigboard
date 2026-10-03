package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func TestRelatedSeparatesCommitAndContributorEvidence(t *testing.T) {
	m := monorepoFixture()
	m.allRecords = append(m.allRecords,
		git.CommitRecord{CommitID: "independent", Author: "Ada", Email: "ada@x", RepoID: "/mono", Date: time.Now(), Subject: "Independent billing work", Changes: []git.PathChange{{Path: "services/billing/other.go"}}},
		git.CommitRecord{Author: "Ada", Email: "ada@x", RepoID: "/mono", Date: time.Now(), Subject: "No object ID", Changes: []git.PathChange{{Path: "services/auth/noid.go"}, {Path: "services/billing/noid.go"}}},
	)
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "3")
	rows := m.glanceCochangedAreas()
	if len(rows) != 1 || rows[0].area.ID != "auto:apps/web" || len(rows[0].records) != 1 || rows[0].records[0].CommitID != "auth" {
		t.Fatalf("cochange inferred from identity or missing IDs: %+v", rows)
	}
	if len(m.glanceRelatedAreas()) != 3 {
		t.Fatal("independent contributor overlap lost")
	}
	m.width, m.height = 120, 36
	view := ansi.Strip(m.View())
	for _, want := range []string{"WHAT CHANGED TOGETHER", "Renew sessions", "Changed areas: apps/web · services/auth", "CONTRIBUTOR OVERLAP", "services/billing", "Ada"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	m = pressAwareness(m, "enter")
	if len(m.glanceEvidence()) != 1 || m.glanceEvidence()[0].CommitID != "auth" {
		t.Fatal("drilldown must contain exactly shared commits")
	}
	m = pressAwareness(m, "enter")
	if !m.showPaths || !strings.Contains(m.View(), "apps/web/login.tsx") || !strings.Contains(m.View(), "services/auth/session.go") {
		t.Fatal("canonical evidence not reachable")
	}
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "esc")
	if m.glance.frame.areaID != "auto:services/auth" || m.glance.frame.tab != glanceRelated {
		t.Fatal("back lost Related source")
	}
}

func TestWorklistSharedUnavailableStatusAndTitleSpace(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 120, 36
	m.prState.errors = map[string]error{"/mono": errors.New("gh not installed")}
	view := ansi.Strip(m.View())
	if strings.Count(view, "PRs: unavailable") != 1 || strings.Contains(view, "? unknown") || strings.Contains(view, "OPEN PRs") {
		t.Fatalf("shared status duplicated:\n%s", view)
	}
	_, _, subject, _ := worklistColumnWidths(116)
	if subject < 45 {
		t.Fatalf("title cramped to %d cells", subject)
	}
}

func TestRelatedSelectionVisibleAcrossSizes(t *testing.T) {
	m := monorepoFixture()
	for i := 0; i < 30; i++ {
		r := m.allRecords[0]
		r.CommitID = string(rune('a'+i)) + "shared"
		r.Changes = []git.PathChange{{Path: "services/auth/session.go"}, {Path: "area" + string(rune('A'+i)) + "/file"}}
		m.allRecords = append(m.allRecords, r)
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "3")
	for _, size := range [][2]int{{120, 36}, {80, 30}, {40, 18}} {
		m.width, m.height = size[0], size[1]
		for _, key := range []string{"home", "down", "pgdown", "end"} {
			m = pressAwareness(m, key)
			rows := m.glanceCochangedAreas()
			selected := m.glanceSelected(m.glanceDetailIDs())
			view := m.View()
			if !strings.Contains(ansi.Strip(view), rows[selected].area.Name) {
				t.Fatalf("selected row invisible at %v", size)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("width overflow")
				}
			}
			if lipgloss.Height(view) > size[1] {
				t.Fatal("height overflow")
			}
		}
	}
}

func TestOverlapOnlyDrilldownNeverClaimsSharedCommits(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "3")
	m = pressAwareness(m, "o")
	m.glance.frame.ids[glanceRelated] = "auto:root"
	// Select the independent README area by its returned identity.
	for _, r := range m.glanceRelatedAreas() {
		if r.area.Name == "Repository root" {
			m.glance.frame.ids[glanceRelated] = r.area.ID
		}
	}
	m = pressAwareness(m, "enter")
	if !m.glance.frame.relatedPeople || m.glance.frame.tab != glancePeople {
		t.Fatal("overlap must open canonical People")
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "Commits touching both") || strings.Count(view, "Contributor identities in both areas") != 1 {
		t.Fatalf("overlap misrepresented:\n%s", view)
	}
	m = pressAwareness(m, "1")
	records := m.glanceEvidence()
	if len(records) != 1 || records[0].CommitID != "root" {
		t.Fatalf("independent identity evidence lost: %+v", records)
	}
	m = pressAwareness(m, "esc")
	if !m.glance.frame.relatedOverlap || m.glance.frame.tab != glanceRelated {
		t.Fatal("back lost overlap lens")
	}
	m.width, m.height = 40, 18
	if !strings.Contains(m.View(), "Ada") {
		t.Fatal("compact overlap hides contributor names")
	}
}

func TestWorklistHasNoCollaborationAssessment(t *testing.T) {
	m := monorepoFixture()
	m.width, m.height = 120, 36
	m.selectedAreaID = "auto:services/auth"
	for _, key := range []string{"", "enter", "3", "o", "enter"} {
		if key != "" {
			m = pressAwareness(m, key)
		}
		view := strings.ToLower(ansi.Strip(m.View()))
		for _, removed := range []string{"collaboration", "review participation", "co-authorship", "worked independently", "incidental"} {
			if strings.Contains(view, removed) {
				t.Fatalf("%s retained removed assessment %q", key, removed)
			}
		}
	}
}
