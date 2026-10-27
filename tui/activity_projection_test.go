package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestActivityProjectionPreservesScopedCanonicalCounts(t *testing.T) {
	now := time.Now()
	m := Model{viewMode: ViewAwareness, width: 120, height: 40, timeIdx: len(TimePresets) - 1}
	m.local.repositories = []git.Repository{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}, {ID: "quiet", Name: "quiet"}}
	shallow := git.CommitRecord{CommitID: "shared", RepoID: "a", RepoName: "alpha", Author: "Alias", Email: "alias@test", Date: now, LinesUnknown: true}
	full := git.CommitRecord{CommitID: "shared", RepoID: "b", RepoName: "beta", Author: "Canonical", Email: "canonical@test", Date: now.Add(-48 * time.Hour), Added: 7, Removed: 3}
	missing := git.CommitRecord{RepoID: "a", RepoName: "alpha", Author: "Missing", Date: now, Added: 2}
	bot := git.CommitRecord{CommitID: "bot", RepoID: "a", RepoName: "alpha", Author: "runner[bot]", Email: "bot@test", Date: now, Added: 5}
	m.local.records = []git.CommitRecord{shallow, missing, full, missing, bot}
	for _, test := range []struct {
		name                                     string
		change                                   func(*Model)
		repo                                     string
		commits, people, added, removed, unknown int
	}{
		{"all", func(*Model) {}, "a", 4, 3, 16, 3, 0},
		{"shared copy", func(*Model) {}, "b", 1, 1, 7, 3, 0},
		{"quiet", func(*Model) {}, "quiet", 0, 0, 0, 0, 0},
		{"shared scope", func(m *Model) { m.sharedRepos = filterValues{"a"} }, "a", 4, 3, 9, 0, 1},
		{"time", func(m *Model) { m.timeIdx = 0 }, "a", 4, 3, 9, 0, 1},
		{"person", func(m *Model) { m.activityPeople = filterValues{stats.IdentityID(full)} }, "a", 1, 1, 7, 3, 0},
		{"bots", func(m *Model) { m.activityHideBots = true }, "a", 3, 2, 11, 3, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := m
			test.change(&model)
			projection := model.prepareActivityProjection()
			var row activityRow
			for _, candidate := range projection.rows {
				if candidate.repositoryID == test.repo {
					row = candidate
				}
			}
			added, removed, unknown := repositoryLineTotals(row)
			if row.repositoryID != test.repo || row.commits != test.commits || len(row.people) != test.people || added != test.added || removed != test.removed || unknown != test.unknown {
				t.Fatalf("projection lost scoped evidence: row=%#v lines=%d/%d/%d", row, added, removed, unknown)
			}
		})
	}
}

func TestActivityProjectionDoesNotSurviveStateChanges(t *testing.T) {
	m := directoryFixture()
	before := m.prepareActivityProjection()
	oldPeople := append([]stats.AuthorStats(nil), before.rows[0].people...)
	m.activityPeople = filterValues{stats.IdentityID(m.local.records[0])}
	m.openDirectoryRepository("/mono")
	after := m.prepareActivityProjection()
	var services activityRow
	for _, row := range after.rows {
		if row.scopeID == "dir:services" {
			services = row
		}
	}
	if !after.directory || services.scopeID == "" || len(services.people) != 1 {
		t.Fatalf("new scope/filter inherited previous projection: %#v", after)
	}
	if before.directory || !reflect.DeepEqual(before.rows[0].people, oldPeople) {
		t.Fatal("preparing a new state changed the previous rows")
	}
	added, removed, _ := repositoryLineTotals(services)
	if added != 60 || removed != 40 {
		t.Fatalf("directory projection used whole-commit counts: %d/%d", added, removed)
	}
	m.directory.frame.path = "services/auth"
	files := m.prepareActivityProjection()
	if len(files.rows) != 1 || files.rows[0].scopeID != "file:services/auth/session.go" {
		t.Fatalf("directory path did not reproject: %#v", files.rows)
	}
}
