package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/richhaase/bigboard/git"
)

func identityMergeFixture() Model {
	m := awarenessFixture()
	m.viewMode = ViewAggregate
	m.timeIdx = 0
	m.local.records[0].Author, m.local.records[0].Email = "Current Name", "primary@example.com"
	m.local.records[1].Author, m.local.records[1].Email, m.local.records[1].Date = "Old Name", "alias@example.com", time.Now().Add(-180*24*time.Hour)
	m.local.records[2].Author, m.local.records[2].Email = "Missing Email", ""
	m.recomputeAuthors()
	m.selectContributor("email:primary@example.com")
	return m
}

func selectMergeEmail(t *testing.T, m Model, email string) Model {
	t.Helper()
	for i, choice := range m.identityMerge.visibleChoices() {
		if choice.email == email {
			m.identityMerge.cursor = i
			return m
		}
	}
	t.Fatalf("email %s absent from choices", email)
	return m
}

func stagedIdentityMerge(t *testing.T) Model {
	t.Helper()
	m := keyboardContractPress(t, identityMergeFixture(), "m")
	m = selectMergeEmail(t, m, "alias@example.com")
	return keyboardContractPress(t, m, " ")
}

func TestIdentityMergeUsesAllCollectedEmailsAndStagesChoices(t *testing.T) {
	m := keyboardContractPress(t, identityMergeFixture(), "m")
	if !m.identityMerge.open || m.identityMerge.primary != "primary@example.com" || len(m.identityMerge.choices) != 3 {
		t.Fatalf("wrong seed or filtered identities: %+v", m.identityMerge)
	}
	before := m
	m = selectMergeEmail(t, m, "alias@example.com")
	m = keyboardContractPress(t, m, " ", "k")
	if !m.identityMerge.selected["alias@example.com"] || m.identityMerge.primary != "alias@example.com" || before.identityMerge.selected["alias@example.com"] {
		t.Fatal("primary/selection did not stage independently")
	}
	m = selectMergeEmail(t, m, "")
	m = keyboardContractPress(t, m, " ")
	if !strings.Contains(m.identityMerge.error, "no valid email") || len(m.identityMerge.selected) != 2 {
		t.Fatal("missing-email identity became mergeable")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.identityMerge.open || len(m.options.Identities) != 0 {
		t.Fatal("cancel applied pending choices")
	}
}

func TestIdentityMergeRequiresReviewAndConfirmationAndRetainsFailedDraft(t *testing.T) {
	m := stagedIdentityMerge(t)
	before := m
	calls := 0
	m.options.SaveIdentities = func(identities map[string]git.Identity) error {
		calls++
		if !reflect.DeepEqual(identities["primary@example.com"].Aliases, []string{"alias@example.com"}) {
			t.Fatal("saved mapping lost selected alias")
		}
		return errors.New("configuration changed elsewhere")
	}
	m, cmd := applyLocal(t, m, key("enter"))
	if cmd != nil || !m.identityMerge.review || calls != 0 || len(m.options.Identities) != 0 {
		t.Fatal("review saved without confirmation")
	}
	m, cmd = applyLocal(t, m, key("enter"))
	if cmd == nil || !m.identityMerge.saving || calls != 0 {
		t.Fatal("confirmation did not schedule persistence")
	}
	m, next := applyLocal(t, m, cmd())
	if next != nil || calls != 1 || m.identityMerge.saving || !m.identityMerge.review || !strings.Contains(m.identityMerge.error, "Not saved") {
		t.Fatal("save failure did not preserve an actionable draft")
	}
	if len(m.options.Identities) != 0 || !reflect.DeepEqual(m.local.records, before.local.records) || !m.identityMerge.selected["alias@example.com"] {
		t.Fatal("failed save changed options or data")
	}
	m = keyboardContractPress(t, m, "esc")
	if m.identityMerge.review || !m.identityMerge.open {
		t.Fatal("failed save trapped review state")
	}
}

func TestIdentityMergeTextHelpAndBoardCancellation(t *testing.T) {
	m := stagedIdentityMerge(t)
	m = keyboardContractPress(t, m, "n", "c", "a", "p", "q", "?", " ", "Name")
	if !m.identityMerge.open || m.identityMerge.input != "capq? Name" || m.quitting || m.showHelp {
		t.Fatal("name editor interpreted shortcut letters")
	}
	m = keyboardContractPress(t, m, "f1")
	if !m.showHelp || m.currentHelp().title != "Contributor identity text" {
		t.Fatal("merge text help unavailable")
	}
	m = keyboardContractPress(t, m, "esc", "enter")
	if m.identityMerge.name != "capq? Name" || m.identityMerge.edit != "" {
		t.Fatal("name edit was not staged")
	}
	m = keyboardContractPress(t, m, "/", "alias", "enter")
	if got := m.identityMerge.visibleChoices(); len(got) != 1 || got[0].email != "alias@example.com" {
		t.Fatal("find does not include email")
	}
	m = keyboardContractPress(t, m, "p")
	if m.identityMerge.open || !m.showPRs || len(m.options.Identities) != 0 {
		t.Fatal("board switch retained unconfirmed choices")
	}
	m = keyboardContractPress(t, m, "m")
	if m.identityMerge.open {
		t.Fatal("PR board opened Contributors action")
	}
}

func TestIdentitySaveRestartsOnlyLocalGenerationAndPreservesCoverage(t *testing.T) {
	m := stagedIdentityMerge(t)
	m.scanContext, m.cancelScans = context.WithCancel(context.Background())
	t.Cleanup(m.cancelScans)
	m.resetPending()
	oldContext, oldGeneration := m.localScanContext, m.scan.generation
	prContext, cancelPR := context.WithCancel(m.scanContext)
	t.Cleanup(cancelPR)
	m.cancelPRs = cancelPR
	m.prState.loading = true
	m.activityPeople = filterValues{"email:alias@example.com", "email:primary@example.com"}
	m.personID, m.activeAuthorID = "email:alias@example.com", "email:alias@example.com"
	m.glance.stack = []glanceFrame{{personID: "email:alias@example.com", ids: [4]string{"", "email:alias@example.com"}}}
	m.local.failed = []string{"api"}
	m.local.errors = map[string]string{"/api": "prior fetch failure"}
	m.local.staleRepos["/api"] = true
	oldScanTime := m.local.scannedAt["/api"]
	m.options.SaveIdentities = func(map[string]git.Identity) error { return nil }
	m, _ = applyLocal(t, m, key("enter"))
	m, save := applyLocal(t, m, key("enter"))
	m, scan := applyLocal(t, m, save())
	if scan == nil || oldContext.Err() == nil || prContext.Err() != nil || !m.scan.localOnly || m.scan.generation == oldGeneration {
		t.Fatal("identity save did not isolate local scan cancellation")
	}
	if m.personID != "email:primary@example.com" || m.activeAuthorID != "email:primary@example.com" || !reflect.DeepEqual(m.activityPeople, filterValues{"email:primary@example.com"}) || m.glance.stack[0].personID != "email:primary@example.com" || m.glance.stack[0].ids[1] != "email:primary@example.com" {
		t.Fatal("saved contributor selections were not remapped")
	}
	remaining := m.scan.remaining
	m, _ = applyLocal(t, m, RepoLoadedMsg{Generation: oldGeneration, Repository: m.repositories[0]})
	if m.scan.remaining != remaining {
		t.Fatal("previous identity generation was admitted")
	}
	for _, repo := range m.repositories {
		record := git.CommitRecord{CommitID: repo.ID, RepoID: repo.ID, Author: "Merged", Email: "primary@example.com", Date: time.Now()}
		var cmd tea.Cmd
		m, cmd = applyLocal(t, m, RepoLoadedMsg{Generation: m.scan.generation, Repository: repo, Records: []git.CommitRecord{record}})
		if m.scan.remaining == 0 && cmd != nil {
			t.Fatal("identity rescan triggered a PR fetch")
		}
	}
	if !m.local.staleRepos["/api"] || m.local.errors["/api"] != "prior fetch failure" || !m.local.scannedAt["/api"].Equal(oldScanTime) || !reflect.DeepEqual(m.local.failed, []string{"api"}) {
		t.Fatal("local-only rescan cleared existing fetch coverage")
	}
	if m.selectedContributorID() != "email:primary@example.com" || m.viewMode != ViewAggregate || m.timeIdx != 0 {
		t.Fatal("identity save lost selected canonical contributor or scope")
	}
}

func TestIdentityMergeModalFitsAndReviewShowsFullEmails(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		m := stagedIdentityMerge(t)
		m.width, m.height = size[0], size[1]
		for _, phase := range []string{"choose", "review", "name", "help"} {
			next := m
			switch phase {
			case "review":
				next = keyboardContractPress(t, next, "enter")
			case "name":
				next = keyboardContractPress(t, next, "n", "Name")
			case "help":
				next = keyboardContractPress(t, next, "?")
			}
			view := ansi.Strip(next.View())
			lines := strings.Split(view, "\n")
			if len(lines) != m.height || !strings.Contains(view, "Contributor") && !strings.Contains(view, "contributor") {
				t.Fatalf("%v %s modal lost frame: %s", size, phase, view)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.width {
					t.Fatalf("%v %s modal overflow", size, phase)
				}
			}
		}
	}
	longEmail := strings.Repeat("long", 15) + "@example.com"
	f := identityMergeState{primary: "primary@example.com", pending: map[string]git.Identity{"primary@example.com": {Aliases: []string{longEmail}}}}
	var reconstructed strings.Builder
	for _, line := range f.reviewLines(30) {
		if ansi.StringWidth(line) > 30 || strings.Contains(line, "…") {
			t.Fatal("review shortened an email")
		}
		reconstructed.WriteString(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "[x] "), "[P] ")))
	}
	if !strings.Contains(reconstructed.String(), longEmail) {
		t.Fatal("full alias cannot be reviewed")
	}
}
