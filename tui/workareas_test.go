package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func monorepoFixture() Model {
	now := time.Now()
	m := awarenessFixture()
	m.repositories = []git.Repository{{ID: "/mono", Name: "mono"}}
	m.loadedRepos = m.repositories
	m.selectedRepoID = "/mono"
	m.areaRepoID = "/mono"
	m.selectedAreaID = ""
	m.allRecords = []git.CommitRecord{
		{CommitID: "auth", Author: "Ada", Email: "ada@x", Date: now, RepoID: "/mono", RepoName: "mono", Subject: "Renew sessions", Changes: []git.PathChange{{Path: "services/auth/session.go"}, {Path: "apps/web/login.tsx"}}},
		{CommitID: "billing", Author: "Grace", Email: "grace@x", Date: now.Add(-time.Hour), RepoID: "/mono", RepoName: "mono", Subject: "Reconcile invoices", Changes: []git.PathChange{{Path: "services/billing/invoice.go"}}},
		{CommitID: "root", Author: "Ada", Email: "ada@x", Date: now.Add(-2 * time.Hour), RepoID: "/mono", RepoName: "mono", Subject: "Document setup", Changes: []git.PathChange{{Path: "README.md"}}},
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	return m
}

func TestMonorepoAreasAndCrossAreaEvidence(t *testing.T) {
	m := monorepoFixture()
	m.selectedAreaID = "auto:services/auth"
	m.personID = "email:ada@x"
	out := m.View()
	foundBilling := false
	for _, a := range m.currentWorkAreas() {
		if a.Name == "services/billing" {
			foundBilling = true
		}
	}
	if !foundBilling {
		t.Fatal("billing area missing")
	}
	for _, want := range []string{"WORK AREAS", "services/auth", "apps/web", "Renew sessions"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	evidence := m.evidence("auto:services/auth", m.personID)
	if len(evidence) != 1 || len(evidence[0].Changes) != 1 || evidence[0].Changes[0].Path != "services/auth/session.go" {
		t.Fatalf("wrong area evidence: %+v", evidence)
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "3")
	if !strings.Contains(m.View(), "apps/web") || !strings.Contains(m.View(), "shared-commit evidence") {
		t.Fatal("related area evidence unavailable")
	}
	m = pressAwareness(m, "1")
	m = pressAwareness(m, "enter")
	if !strings.Contains(m.View(), "All changed paths") || !strings.Contains(m.View(), "apps/web/login.tsx") {
		t.Fatal("full commit paths unavailable in inspector")
	}

}

func TestSingleRepositoryOpensAreasAndEscReturns(t *testing.T) {
	m := monorepoFixture()
	m.areaRepoID = ""
	m.openSingleRepositoryAreas()
	if m.areaRepoID != "/mono" {
		t.Fatal("single repo did not open areas")
	}
	m = pressAwareness(m, "esc")
	if m.areaRepoID != "" {
		t.Fatal("cannot return to repository overview")
	}
	m = pressAwareness(m, "enter")
	if m.areaRepoID != "/mono" {
		t.Fatal("enter does not open selected repo")
	}
	m = pressAwareness(m, "v")
	if m.viewMode != ViewAggregate {
		t.Fatal("stats inaccessible")
	}
	m = pressAwareness(m, "v")
	if m.areaRepoID != "/mono" {
		t.Fatal("stats roundtrip lost area scope")
	}
}

func TestAreasNamedRulesFilteringAndStableDefinitions(t *testing.T) {
	m := monorepoFixture()
	m.options.WorkAreas = map[string][]stats.WorkAreaRule{"mono": {{Name: "Authentication", Paths: []string{"services/auth", "apps/web"}}}}
	m.rebuildAreaDefinitions()
	areas := m.currentWorkAreas()
	var auth stats.WorkArea
	for _, a := range areas {
		if a.ID == "named:Authentication" {
			auth = a
		}
	}
	if len(auth.Records) != 1 || len(auth.Records[0].Changes) != 2 {
		t.Fatalf("named area=%+v", auth)
	}
	m.selectedAreaID = auth.ID
	m.allRecords[0].Date = time.Now().Add(-30 * 24 * time.Hour)
	m.timeIdx = 0
	m.recomputeAuthors()
	if strings.Contains(m.View(), "Renew sessions") {
		t.Fatal("out-of-range evidence remains")
	}
	m.excludedRepos["/mono"] = true
	m.recomputeAuthors()
	m = pressAwareness(m, "tab")
	if m.areaRepoID != "" {
		t.Fatal("excluded repository still active")
	}
}

func TestAreasCanonicalSharedCommitAndUnknownPaths(t *testing.T) {
	m := monorepoFixture()
	m.loadedRepos = append(m.loadedRepos, git.Repository{ID: "/aaa", Name: "copy"})
	copy := m.allRecords[0]
	copy.RepoID = "/aaa"
	copy.RepoName = "copy"
	copy.Email = "canonical@x"
	copy.Author = "Canonical"
	m.allRecords[0].LinesUnknown = true
	m.allRecords[0].PathsUnknown = true
	m.allRecords[0].Changes = nil
	m.allRecords = append(m.allRecords, copy)
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	records := m.evidence("auto:services/auth", "email:canonical@x")
	if len(records) != 1 || records[0].PathsUnknown || records[0].Author != "Canonical" {
		t.Fatalf("complete canonical copy not used: %+v", records)
	}
	m.excludedRepos["/aaa"] = true
	m.recomputeAuthors()
	areas := m.currentWorkAreas()
	found := false
	for _, a := range areas {
		if a.ID == "unknown" {
			found = true
		}
	}
	if !found {
		t.Fatal("shallow boundary invented areas")
	}
}

func TestAreasCompactEvidenceAndEmptyState(t *testing.T) {
	m := monorepoFixture()
	m.width = 40
	m.height = 18
	m.selectedAreaID = "auto:services/auth"
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	out := m.View()
	if !strings.Contains(out, "Renew sessions") || !strings.Contains(out, "services/auth/session.go") {
		t.Fatalf("compact evidence hidden:\n%s", out)
	}
	if lipgloss.Height(out) > 18 {
		t.Fatal("height overflow")
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatal("width overflow")
		}
	}
	m.allRecords = nil
	m.recomputeAuthors()
	m = pressAwareness(m, "esc")
	m = pressAwareness(m, "esc")
	if !strings.Contains(m.View(), "No work-area activity") {
		t.Fatal("empty area view misreported repository failure")
	}
}

func TestAreaSharedCommitMissingEmailPreservesCanonicalIdentity(t *testing.T) {
	m := monorepoFixture()
	m.allRecords = m.allRecords[:1]
	m.allRecords[0].Email = ""
	r := m.allRecords[0]
	r.RepoID = "/z-copy"
	r.RepoName = "copy"
	m.allRecords = append(m.allRecords, r)
	m.loadedRepos = append(m.loadedRepos, git.Repository{ID: "/z-copy", Name: "copy"})
	m.areaRepoID = "/z-copy"
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	areas := m.currentWorkAreas()
	if len(areas) == 0 {
		t.Fatal("missing-email shared commit disappeared from associated repo")
	}
	for _, a := range areas {
		for _, r := range a.Records {
			if stats.IdentityID(r) != m.authors[0].ID {
				t.Fatal("canonical identity rewritten")
			}
		}
	}
}

func TestAreaPathInspectorScrollsEveryLiteralPath(t *testing.T) {
	m := monorepoFixture()
	m.width = 40
	m.height = 18
	m.selectedAreaID = "auto:services/auth"
	m.awarenessPane = 2
	for i := 0; i < 30; i++ {
		m.allRecords[0].Changes = append(m.allRecords[0].Changes, git.PathChange{Path: "services/auth/" + strings.Repeat("long", i+1) + ".go"})
	}
	m.rebuildAreaDefinitions()
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "enter")
	if !m.showPaths || !strings.Contains(m.View(), "CHANGED PATHS") {
		t.Fatal("path inspector did not open")
	}
	for i := 0; i < 400; i++ {
		m = pressAwareness(m, "down")
	}
	if m.pathOffset == 0 || !strings.Contains(m.View(), ".go") {
		t.Fatal("last long filename not reachable")
	}
	if lipgloss.Height(m.View()) > 18 {
		t.Fatal("path inspector height overflow")
	}
	m = pressAwareness(m, "esc")
	if m.showPaths || m.awarenessPane != 2 {
		t.Fatal("Esc did not return to evidence")
	}
	m = pressAwareness(m, "enter")
	m = pressAwareness(m, "tab")
	if m.showPaths {
		t.Fatal("Tab left path inspector active")
	}
}

func TestAreaViewResizeSweep(t *testing.T) {
	for _, width := range []int{40, 69, 70, 79, 94, 95, 110, 160} {
		for height := 18; height <= 44; height++ {
			m := monorepoFixture()
			m.width = width
			m.height = height
			m.selectedAreaID = "auto:services/auth"
			m = pressAwareness(m, "enter")
			out := m.View()
			if !strings.Contains(out, "Renew sessions") {
				t.Fatalf("evidence hidden at %dx%d", width, height)
			}
			if lipgloss.Height(out) > height {
				t.Fatalf("height overflow at %dx%d", width, height)
			}
			for _, line := range strings.Split(out, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width overflow at %dx%d", width, height)
				}
			}
		}
	}
}
