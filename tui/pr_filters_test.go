package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	gh "github.com/richhaase/bigboard/github"
)

func TestPRFiltersCombineAndClearWithoutChangingInclusion(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	m = keyboardContractPress(t, m, "p", "f", "/", "gateway", "enter", "enter")
	if len(m.visiblePRs()) != 3 || m.prRepoID != "/sample/gateway" {
		t.Fatal("repository search did not select the repository")
	}
	m = keyboardContractPress(t, m, "a", "/", "contributor03", "enter", "enter", "v", "home", "down", "down", "down", "enter", "c", "home", "down", "enter", "d", "end", "enter")
	prs := m.visiblePRs()
	if len(prs) != 1 || prs[0].Number != 102 || !prs[0].Draft {
		t.Fatalf("combined filters returned wrong PRs: %+v", prs)
	}
	for _, want := range []string{"Author:", "Review:", "Checks:", "State:"} {
		if !strings.Contains(ansi.Strip(m.View()), want) {
			t.Fatalf("active filter missing: %s", want)
		}
	}
	m = keyboardContractPress(t, m, "v", "home", "down", "down", "enter")
	if len(m.visiblePRs()) != 0 || !strings.Contains(m.View(), "No matching PRs") || strings.Contains(m.View(), "SELECTED") {
		t.Fatal("contradictory filters did not show an empty result")
	}
	m = keyboardContractPress(t, m, "x")
	if len(m.visiblePRs()) != 48 || len(m.excludedRepos) != 0 || m.prFilters != (prFilters{}) || m.prRepoID != "" {
		t.Fatal("clear did not restore all included PRs")
	}
}

func TestPRFilterPickerOwnsSearchAndCancelsPendingChoices(t *testing.T) {
	m := boardLayoutFixture()
	m.width, m.height = 80, 24
	m = keyboardContractPress(t, m, "p", "a", "/", "w", "p", "q", "d")
	if !m.showPRs || !m.prRepoPicker || !m.prFilterSearch || m.prFilterQuery != "wpqd" || m.quitting {
		t.Fatal("filter search leaked a command")
	}
	m = keyboardContractPress(t, m, "f1", "esc")
	if !m.prFilterSearch {
		t.Fatal("help dismissal lost filter search")
	}
	m = keyboardContractPress(t, m, "esc", "end", "esc")
	if m.prFilters != (prFilters{}) || m.prRepoPicker {
		t.Fatal("cancel applied pending author")
	}
	m = keyboardContractPress(t, m, "f", "tab")
	if m.prFilterKind != "author" {
		t.Fatal("Tab did not advance filter category")
	}
	m = keyboardContractPress(t, m, "end", "w")
	if m.showPRs || m.prRepoPicker || m.prFilters != (prFilters{}) {
		t.Fatal("board switch applied pending filter")
	}
}

func TestPRUnknownFiltersDoNotImplyApproval(t *testing.T) {
	pr := gh.PullRequest{ReviewDecision: "UNRECOGNIZED", CheckState: "", Draft: true}
	if !(prFilters{author: "?", review: "Unknown", checks: "Unknown", state: "draft"}).matches(pr) {
		t.Fatal("unknown evidence disappeared from explicit unknown filters")
	}
	for _, f := range []prFilters{{review: "Approved"}, {checks: "Passing"}, {state: "open"}} {
		if f.matches(pr) {
			t.Fatal("unknown or draft PR matched a stronger state")
		}
	}
	pr.Author = "Contributor"
	if !(prFilters{author: "contributor"}).matches(pr) {
		t.Fatal("author filtering is case sensitive")
	}
}

func TestPRNumberDraftAndSuccessPresentation(t *testing.T) {
	previous := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previous)
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := boardLayoutFixture()
	m.width, m.height = 120, 36
	pr := gh.PullRequest{Repo: "sample/gateway", Number: 123, Title: "Review this change", Draft: true, ReviewDecision: "APPROVED", CheckState: "SUCCESS"}
	green := "\x1b[38;2;157;219;162m"
	for _, selected := range []bool{false, true} {
		row := m.prTableRow(pr, "gateway", selected, 0, m.width)[0]
		if strings.Count(row, green) < 2 {
			t.Fatal("approved and passing are not both green")
		}
		plain := ansi.Strip(row)
		l := resolvePRTable(m.width)
		numberStart := 4 + l.repo + 1
		titleStart := numberStart + l.number + 1 + l.state + 1
		if ansi.Cut(plain, numberStart, numberStart+l.number) != "#123   " || !strings.HasPrefix(ansi.Cut(plain, titleStart, titleStart+l.title), pr.Title) {
			t.Fatal("number and title do not occupy separate columns")
		}
		if !strings.Contains(plain, "◇") || strings.Contains(plain, "DRAFT") {
			t.Fatal("draft state is not represented by the dedicated symbol")
		}
	}
	for _, size := range [][2]int{{40, 18}, {80, 24}, {120, 36}} {
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "p", "a", "/", "contributor", "enter")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Fatal("filter picker lost its frame")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("filter picker overflowed")
			}
		}
	}
}

func TestPRFilterModalPreservesBoardAndPagesItsOwnViewport(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {40, 18}, {80, 24}, {160, 48}} {
		m := boardLayoutFixture()
		m.width, m.height = size[0], size[1]
		m = keyboardContractPress(t, m, "p", "down")
		before := ansi.Strip(m.View())
		m = keyboardContractPress(t, m, "f")
		view := ansi.Strip(m.View())
		for _, want := range []string{"PR filters · Repository", "[f Repo]", "Find options", "✓ applied", "Enter apply", "Esc cancel", "╭"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%v modal missing %q:\n%s", size, want, view)
			}
		}
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Fatal("filter modal changed terminal height")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("filter modal overflowed terminal width")
			}
		}
		if m.width >= 80 && !strings.Contains(view, "BIGBOARD / Pull requests") {
			t.Fatal("filter picker replaced the board")
		}
		_, page := m.prFilterModalSize(m.width, m.height)
		m = keyboardContractPress(t, m, "pgdown")
		if m.prRepoRow != page {
			t.Fatal("filter paging did not use modal viewport")
		}
		m = keyboardContractPress(t, m, "end")
		options := m.prFilterOptions()
		if m.prRepoRow != len(options)-1 || !strings.Contains(ansi.Strip(m.View()), options[len(options)-1].label) {
			t.Fatal("last option is not visible")
		}
		m = keyboardContractPress(t, m, "?", "esc", "esc")
		if m.prRepoPicker || ansi.Strip(m.View()) != before {
			t.Fatal("help or cancellation changed the underlying board")
		}
	}
}
