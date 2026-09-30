package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

func reviewBusyFixture() Model {
	m := monorepoFixture()
	m.allRecords = nil
	now := time.Now()
	for i := 0; i < 80; i++ {
		m.allRecords = append(m.allRecords, git.CommitRecord{CommitID: fmt.Sprintf("review-oid-%03d", i), Author: "Ada", Email: "ada@x", Date: now.Add(-time.Duration(i) * 24 * time.Hour), RepoID: "/mono", RepoName: "mono", Subject: fmt.Sprintf("Unique review subject %03d", i), Changes: []git.PathChange{{Path: fmt.Sprintf("feature/child%02d/file.go", i%10)}}})
	}
	m.recomputeAuthors()
	m.rebuildAreaDefinitions()
	m.selectedAreaID = "auto:feature"
	return pressAwareness(m, "enter")
}

func TestReviewActivitySelectedVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 18}, {70, 24}, {110, 40}} {
		m := reviewBusyFixture()
		m.width = size[0]
		m.height = size[1]
		for i := 0; i < 80; i++ {
			want := fmt.Sprintf("Unique review subject %03d", i)
			if !strings.Contains(m.View(), want) {
				t.Fatalf("selected %d invisible at %dx%d:\n%s", i, m.width, m.height, m.View())
			}
			m = pressAwareness(m, "down")
		}
	}
}

func TestReviewInspectorNeverRetargetsRemovedCommit(t *testing.T) {
	m := reviewBusyFixture()
	m = pressAwareness(m, "enter")
	if !m.showPaths {
		t.Fatal("inspector not open")
	}
	if !strings.Contains(m.View(), "review-oid-000") {
		t.Fatal("initial selection wrong")
	}
	kept := append([]git.CommitRecord(nil), m.allRecords[1:]...)
	m.loading = true
	m.resetPending()
	next, _ := m.Update(RepoLoadedMsg{Generation: m.scanGeneration, Repository: m.repositories[0], Records: kept})
	m = next.(Model)
	if m.showPaths && strings.Contains(m.View(), "review-oid-001") {
		t.Fatalf("removed inspector silently retargeted next commit:\n%s", m.View())
	}
}
