package tui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/richhaase/bigboard/github"
)

// Opt-in terminal QA only. Normal tests skip this interactive harness. It uses
// the real model/update/render path with explicitly synthetic stress evidence;
// the production binary has no fixture mode or alternate data source.
func TestWorklistTerminalCapture(t *testing.T) {
	mode := os.Getenv("BIGBOARD_TEST_CAPTURE")
	if mode == "" {
		t.Skip("interactive terminal capture")
	}
	m := glanceBusyFixture()
	m.scannedAt["/mono"] = time.Now().Add(-2 * time.Hour)
	m.staleRepos["/mono"] = true
	for i := range m.allRecords {
		r := &m.allRecords[i]
		r.Date = r.Date.Add(-3 * time.Hour)
		if i%120 == 0 {
			r.Author = "Alexandria 長い名前 García \u0301"
		}
		r.Subject = "Repair authentication recovery · 認証を修復 · " + r.Subject + " with long trailing context"
		r.Changes[0].Path = strings.Replace(r.Changes[0].Path, "feature/", "feature-with-a-long-name-認証/", 1)
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:feature-with-a-long-name-認証"
	m.prState = prState{repositories: map[string]string{"/mono": "example/stress"}, snapshots: map[string]prSnapshot{
		"example/stress": {checked: time.Now(), lastGood: time.Now().Add(-time.Hour), partial: true, err: errors.New("QA retained partial snapshot"), prs: []gh.PullRequest{
			{Repo: "example/stress", Number: 218, Title: "Restore 認証 flow with a deliberately long review title", Author: "alexandria-long-handle", CheckState: "FAILURE", ReviewDecision: "REVIEW_REQUIRED", Files: []gh.File{{Path: "feature-with-a-long-name-認証/session.go"}}, FilesComplete: true, ContextComplete: true},
			{Repo: "example/stress", Number: 214, Title: "Preserve session history", Author: "maya", CheckState: "PENDING", ReviewDecision: "REVIEW_REQUIRED", Files: []gh.File{{Path: "feature-with-a-long-name-認証/session.go"}}, FilesComplete: true, ContextComplete: true},
		}},
	}}
	if mode == "detail" {
		m = pressAwareness(m, "enter")
	}
	if _, err := tea.NewProgram(worklistCaptureModel{m}, tea.WithAltScreen()).Run(); err != nil {
		t.Fatal(err)
	}
}

type worklistCaptureModel struct{ Model }

func (m worklistCaptureModel) Init() tea.Cmd { return nil }
func (m worklistCaptureModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	return worklistCaptureModel{next.(Model)}, cmd
}
