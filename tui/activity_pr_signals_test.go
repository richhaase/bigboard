package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gh "github.com/richhaase/bigboard/github"
)

func TestActivityPRSignalsUseSharedScopeAndSeparateStatusCounts(t *testing.T) {
	m := prFixture()
	m.timeIdx = 0
	m.activityPeople = filterValues{"someone-else"}
	m.activityHideBots = true
	m.prFilters.author = filterValues{"someone-else"}
	m.prQuery = "unmatched"
	snapshot := m.prState.snapshots["org/repo"]
	base := snapshot.prs[0]
	base.CheckState, base.ReviewDecision = "FAILURE", "CHANGES_REQUESTED"
	snapshot.prs = []gh.PullRequest{base}
	for i, check := range []string{"ERROR", "PENDING", "SUCCESS"} {
		pr := base
		pr.Number, pr.CheckState, pr.ReviewDecision = i+8, check, "APPROVED"
		snapshot.prs = append(snapshot.prs, pr)
	}
	old := base
	old.Number, old.UpdatedAt = 11, time.Now().Add(-25*time.Hour)
	snapshot.prs = append(snapshot.prs, old)
	m.prState.snapshots["org/repo"] = snapshot
	for _, repo := range []string{"/api", "/web"} {
		got := m.activityPRSignal(activityRow{repositoryID: repo, scopeID: repo})
		if got != (activityPRSignal{open: 4, failing: 2, changes: 1, known: true}) {
			t.Fatalf("shared scope or alias changed status counts: %+v", got)
		}
	}
	m.sharedRepos = filterValues{"/web"}
	if got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: "/api"}); got.known {
		t.Fatalf("view-excluded repository yielded evidence: %+v", got)
	}
}

func TestActivityPRSignalsDistinguishInventoryFromFileCoverage(t *testing.T) {
	m := prFixture()
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[0].FilesComplete = false
	snapshot.prs[0].Files = []gh.File{{Path: "feature/new.go"}, {Path: "ui/new.go"}, {Path: "vendor/dependency.go"}}
	m.prState.snapshots["org/repo"] = snapshot
	for _, tc := range []struct {
		scope string
		open  int
	}{
		{"dir:feature", 1},
		{"file:feature/new.go", 1},
		{"dir:other", 0},
		{"dir:feat", 0},
		{"file:feature/new", 0},
		{"dir:vendor", 0},
	} {
		got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: tc.scope})
		if got.open != tc.open || !got.known || !got.uncertain || !strings.HasSuffix(ansi.Strip(got.render(false)), "?") {
			t.Fatalf("%s lost incomplete path coverage: %+v", tc.scope, got)
		}
	}
	if got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: "/api"}); got.open != 1 || got.uncertain {
		t.Fatalf("repository count depends on file completeness: %+v", got)
	}
	m.options.IncludeGenerated = true
	if got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: "dir:vendor"}); got.open != 1 {
		t.Fatalf("all-files option ignored: %+v", got)
	}
	snapshot.prs[0].FilesComplete = true
	snapshot.prs[0].ContextComplete = false
	m.prState.snapshots["org/repo"] = snapshot
	if got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: "dir:other"}); got.open != 0 || got.uncertain {
		t.Fatalf("complete current paths depend on unrelated review context: %+v", got)
	}
}

func TestActivityPRSignalsUnavailableStaleAndQuiet(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prepare   func(*Model)
		known     bool
		uncertain bool
		text      string
	}{
		{"known empty", func(m *Model) { m.prState.snapshots["org/repo"] = prSnapshot{} }, true, false, "—"},
		{"missing", func(m *Model) { m.prState.snapshots = nil }, false, false, "?"},
		{"first failure", func(m *Model) { m.prState.snapshots["org/repo"] = prSnapshot{err: errors.New("offline")} }, false, true, "?"},
		{"first partial empty", func(m *Model) { m.prState.snapshots["org/repo"] = prSnapshot{partial: true} }, false, true, "?"},
		{"retained empty", func(m *Model) {
			m.prState.snapshots["org/repo"] = prSnapshot{err: errors.New("offline"), lastGood: time.Now()}
		}, true, true, "0?"},
		{"retained inventory", func(m *Model) {
			s := m.prState.snapshots["org/repo"]
			s.err = errors.New("offline")
			m.prState.snapshots["org/repo"] = s
		}, true, true, "1?"},
		{"partial inventory", func(m *Model) {
			s := m.prState.snapshots["org/repo"]
			s.partial = true
			m.prState.snapshots["org/repo"] = s
		}, true, true, "1?"},
		{"origin failed", func(m *Model) { m.prState.errors = map[string]error{"/api": errors.New("missing origin")} }, true, true, "1?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := prFixture()
			tc.prepare(&m)
			got := m.activityPRSignal(activityRow{repositoryID: "/api", scopeID: "/api"})
			if got.known != tc.known || got.uncertain != tc.uncertain || ansi.Strip(got.render(false)) != tc.text {
				t.Fatalf("wrong coverage: %+v %q", got, ansi.Strip(got.render(false)))
			}
		})
	}
}

func TestActivityAreaPRSummaryKeepsUnknownUnmatchedPathsVisible(t *testing.T) {
	m := prFixture()
	m.areaRepoID = "/api"
	snapshot := m.prState.snapshots["org/repo"]
	snapshot.prs[0].FilesComplete = false
	m.prState.snapshots["org/repo"] = snapshot
	if got := m.prGlanceSignal("auto:other"); got != "0 known open · areas unknown" {
		t.Fatalf("unmatched incomplete files became an empty area: %s", got)
	}
	if got := m.prGlanceSignal("/api"); got != "1 open · 1 pending" {
		t.Fatalf("repository inventory was qualified by file coverage: %s", got)
	}
}

func TestActivityPRSignalKeepsEveryWideCountQualified(t *testing.T) {
	signal := activityPRSignal{open: 200, failing: 200, changes: 200, known: true, uncertain: true}
	text := ansi.Strip(signal.render(true))
	if strings.Join(strings.Fields(text), " ") != "200? 200? 200?" || ansi.StringWidth(text) != repositoryOverviewWidths(150).prs {
		t.Fatalf("maximum retained counts lost qualifiers or overflowed: %s", text)
	}
}

func TestActivityPRSignalQuietCellsAndAlignment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signal activityPRSignal
		cells  [3]string
	}{
		{"empty", activityPRSignal{known: true}, [3]string{"—", "", ""}},
		{"quiet checks", activityPRSignal{known: true, open: 12}, [3]string{"12", "", ""}},
		{"warnings", activityPRSignal{known: true, open: 12, failing: 3, changes: 4}, [3]string{"12", "3", "4"}},
		{"unknown", activityPRSignal{}, [3]string{"?", "?", "?"}},
		{"uncertain zero", activityPRSignal{known: true, uncertain: true}, [3]string{"0?", "0?", "0?"}},
		{"uncertain warnings", activityPRSignal{known: true, uncertain: true, open: 12, failing: 3}, [3]string{"12?", "3?", "0?"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := ansi.Strip(tc.signal.render(true))
			start := 0
			for i, width := range []int{activityPROpenWidth, activityPRFailWidth, activityPRChangesWidth} {
				got := ansi.Cut(text, start, start+width)
				if got != strings.Repeat(" ", width-ansi.StringWidth(tc.cells[i]))+tc.cells[i] {
					t.Fatalf("misaligned or misleading cell %d: %q", i, got)
				}
				start += width + 1
			}
		})
	}
}

func TestActivityPRSignalsDateScopeQualifiesOnlyRelevantFiles(t *testing.T) {
	m := prFixture()
	m.timeIdx = 0
	snapshot := m.prState.snapshots["org/repo"]
	old := snapshot.prs[0]
	old.Number, old.UpdatedAt, old.FilesComplete = 8, time.Now().Add(-25*time.Hour), false
	unknown := old
	unknown.Number, unknown.UpdatedAt = 9, time.Time{}
	future := snapshot.prs[0]
	future.Number, future.UpdatedAt = 10, time.Now().Add(48*time.Hour)
	snapshot.prs = append(snapshot.prs, old, unknown, future)
	m.prState.snapshots["org/repo"] = snapshot
	row := activityRow{repositoryID: "/api", scopeID: "dir:feature"}
	if got := m.activityPRSignal(row); got.open != 2 || got.uncertain {
		t.Fatalf("out-of-range unknown paths affected current count: %+v", got)
	}
	m.timeIdx = len(TimePresets) - 1
	if got := m.activityPRSignal(row); got.open != 4 || !got.uncertain {
		t.Fatalf("all-time scope lost old or undated paths: %+v", got)
	}
}

func TestActivityPRSignalColumnsPreserveRowsAndFitTerminals(t *testing.T) {
	for _, width := range []int{36, 65, 66, 76, 95, 96, 116, 149, 150, 176, 236} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := prFixture()
			row := m.glanceRepositories()[0]
			columns := ansi.Strip(m.repositoryOverviewColumns(width))
			lines := m.worklistRepositoryRow(row, true, width, 1, m.repositoryMeasureMax(m.glanceRepositories()))
			if len(lines) != activityRankingRowHeight(width+4) {
				t.Fatal("PR signal changed row height")
			}
			if ansi.StringWidth(columns) > width {
				t.Fatalf("header overflows %d: %s", width, columns)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != width+2 {
					t.Fatalf("row changed width %d: %s", width, ansi.Strip(line))
				}
			}
			if width >= 150 && (!strings.Contains(columns, "OPEN PRs FAIL CHANGES") || strings.Contains(ansi.Strip(lines[0]), "open") || strings.Contains(ansi.Strip(lines[0]), "fail")) {
				t.Fatal("wide PR column lost status meaning")
			}
			if width >= 66 && width < 150 && !strings.Contains(columns, "OPEN PRs") {
				t.Fatal("compact PR count lacks label")
			}
			if width < 66 && strings.Contains(columns, "PRs") {
				t.Fatal("tiny layout includes PR column")
			}
			if width >= 96 && repositoryOverviewWidths(width).bar < 10 {
				t.Fatal("PR column crowded out line bar")
			}
		})
	}
}
