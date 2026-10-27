package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestScanBatchPublicationPolicies(t *testing.T) {
	oldTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	checked := oldTime.Add(time.Hour)
	repo := git.Repository{ID: "/repo", Path: "/repo", Name: "current-name"}
	oldRepo := repo
	oldRepo.Name = "retained-name"
	oldRecord := git.CommitRecord{RepoID: repo.ID, CommitID: "old"}
	newRecord := git.CommitRecord{RepoID: repo.ID, CommitID: "new"}
	scanErr, fetchErr := errors.New("scan failed"), errors.New("fetch failed")
	for _, tc := range []struct {
		name           string
		initial        bool
		empty          bool
		scanErr        error
		fetchErr       error
		wantRecord     string
		wantRepository string
		wantTime       time.Time
		wantStale      bool
		wantFailure    string
		wantFatal      bool
	}{
		{name: "startup success", initial: true, wantRecord: "new", wantRepository: "current-name", wantTime: checked},
		{name: "startup scan failure", initial: true, scanErr: scanErr, wantStale: true, wantFailure: "scan failed", wantFatal: true},
		{name: "startup offline success", initial: true, fetchErr: fetchErr, wantRecord: "new", wantRepository: "current-name", wantTime: checked, wantStale: true, wantFailure: "fetch failed"},
		{name: "startup fetch and scan failure", initial: true, scanErr: scanErr, fetchErr: fetchErr, wantStale: true, wantFailure: "fetch failed scan failed", wantFatal: true},
		{name: "refresh success", wantRecord: "new", wantRepository: "current-name", wantTime: checked},
		{name: "refresh empty success", empty: true, wantRepository: "current-name", wantTime: checked},
		{name: "refresh scan failure", scanErr: scanErr, wantRecord: "old", wantRepository: "retained-name", wantTime: oldTime, wantStale: true, wantFailure: "scan failed"},
		{name: "refresh offline success", fetchErr: fetchErr, wantRecord: "new", wantRepository: "current-name", wantTime: oldTime, wantStale: true, wantFailure: "fetch failed"},
		{name: "refresh fetch and scan failure", scanErr: scanErr, fetchErr: fetchErr, wantRecord: "old", wantRepository: "retained-name", wantTime: oldTime, wantStale: true, wantFailure: "fetch failed scan failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := localSnapshot{}
			if !tc.initial {
				previous = localSnapshot{
					records: []git.CommitRecord{oldRecord}, repositories: []git.Repository{oldRepo},
					failed: []string{"earlier failure"}, errors: map[string]string{repo.ID: "earlier failure"},
					scannedAt: map[string]time.Time{repo.ID: oldTime}, staleRepos: map[string]bool{repo.ID: true},
				}
			}
			batch := newScanBatch([]git.Repository{repo}, 7, tc.initial)
			if premature, ready := batch.publish(previous); ready || !reflect.DeepEqual(premature, previous) {
				t.Fatal("unfinished batch published a snapshot")
			}
			records := []git.CommitRecord{newRecord}
			if tc.empty {
				records = nil
			}
			if !batch.admit(RepoLoadedMsg{Generation: 7, Repository: repo, Records: records, Err: tc.scanErr, FetchErr: tc.fetchErr}, checked) {
				t.Fatal("scheduled result was rejected")
			}
			next, ready := batch.publish(previous)
			if !ready || batch.running || batch.outcomes != nil {
				t.Fatal("completed batch did not publish and release its pending records")
			}
			var recordID, repositoryName string
			if len(next.records) == 1 {
				recordID = next.records[0].CommitID
			}
			if len(next.repositories) == 1 {
				repositoryName = next.repositories[0].Name
			}
			if len(next.records) > 1 || recordID != tc.wantRecord || len(next.repositories) > 1 || repositoryName != tc.wantRepository {
				t.Fatalf("wrong retained/replacement data: %+v", next)
			}
			if !next.scannedAt[repo.ID].Equal(tc.wantTime) || next.staleRepos[repo.ID] != tc.wantStale || next.errors[repo.ID] != tc.wantFailure {
				t.Fatalf("wrong freshness or diagnostic state: %+v", next)
			}
			if tc.wantFailure == "" && len(next.failed) != 0 || tc.wantFailure != "" && !slices.Equal(next.failed, []string{repo.Name}) {
				t.Fatalf("failure names did not use the attempted repository: %v", next.failed)
			}
			if (next.loadError() != nil) != tc.wantFatal {
				t.Fatalf("wrong fatal error: %v", next.loadError())
			}
		})
	}
}

func TestScanBatchCopiesOwnAdmissionAndPublication(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repos := []git.Repository{{ID: "z", Name: "Zulu"}, {ID: "a", Name: "Alpha"}}
	previous := localSnapshot{
		records:      []git.CommitRecord{{RepoID: "z", CommitID: "old-z"}, {RepoID: "a", CommitID: "old-a"}},
		repositories: slices.Clone(repos), scannedAt: map[string]time.Time{"z": at, "a": at},
		staleRepos: map[string]bool{"z": true}, errors: map[string]string{"z": "old failure"}, failed: []string{"Zulu"},
	}
	batch := newScanBatch(repos, 3, false)
	input := []git.CommitRecord{{RepoID: "z", CommitID: "new-z"}}
	batch.admit(RepoLoadedMsg{Generation: 3, Repository: repos[0], Records: input}, at.Add(time.Hour))
	input[0].CommitID = "caller changed its buffer"
	backing := make([]scanOutcome, 3)
	copy(backing, batch.outcomes)
	backing[1].result.Generation = 99
	batch.outcomes = backing[:1]
	fork := batch
	batch.admit(RepoLoadedMsg{Generation: 3, Repository: repos[1], Err: errors.New("unavailable")}, at.Add(2*time.Hour))
	if len(fork.outcomes) != 1 || fork.remaining != 1 || fork.active != 1 || !fork.inFlight["a"] || backing[1].result.Generation != 99 {
		t.Fatal("admission mutated a prior batch's maps or spare slice capacity")
	}
	next, ready := batch.publish(previous)
	if !ready || batch.outcomes != nil || len(fork.outcomes) != 1 {
		t.Fatal("publication failed to release only the completed batch's pending data")
	}
	if len(next.records) != 2 || next.records[0].CommitID != "new-z" || next.records[1].CommitID != "old-a" {
		t.Fatalf("publication changed input ownership or completion order: %+v", next.records)
	}
	if !slices.Equal(next.repositories, []git.Repository{repos[1], repos[0]}) || !slices.Equal(previous.repositories, repos) || previous.records[0].CommitID != "old-z" {
		t.Fatal("publication sorted or replaced a previous snapshot's arrays")
	}
	if !previous.staleRepos["z"] || previous.staleRepos["a"] || !previous.scannedAt["z"].Equal(at) || previous.errors["z"] != "old failure" {
		t.Fatal("publication mutated a previous snapshot's freshness or diagnostics")
	}
	if !fork.admit(RepoLoadedMsg{Generation: 3, Repository: repos[1]}, at.Add(3*time.Hour)) {
		t.Fatal("copy could not independently finish its pending scan")
	}
	other, ready := fork.publish(previous)
	if !ready || len(other.records) != 1 || len(other.failed) != 0 || other.staleRepos["a"] || len(next.records) != 2 || !next.staleRepos["a"] {
		t.Fatal("publishing the copied batch changed the first publication")
	}
	if _, again := batch.publish(previous); again {
		t.Fatal("completed batch published twice")
	}
}

func TestScanBatchOwnsSchedulingAndRejection(t *testing.T) {
	var repos []git.Repository
	for i := range 12 {
		repos = append(repos, git.Repository{ID: fmt.Sprint(i), Name: fmt.Sprint(i)})
	}
	batch := newScanBatch(repos, 5, false)
	if _, ok := batch.scheduleNext(repos); ok {
		t.Fatal("batch exceeded initial scan capacity")
	}
	for _, result := range []RepoLoadedMsg{
		{Generation: 4, Repository: repos[0]},
		{Generation: 6, Repository: repos[0]},
		{Generation: 5, Repository: repos[11]},
		{Generation: 5, Repository: git.Repository{ID: "unknown"}},
	} {
		if batch.admit(result, time.Now()) || batch.remaining != 12 || batch.active != 8 || len(batch.outcomes) != 0 {
			t.Fatal("invalid result changed the batch")
		}
	}
	result := RepoLoadedMsg{Generation: 5, Repository: repos[7]}
	if !batch.admit(result, time.Now()) || batch.admit(result, time.Now()) {
		t.Fatal("scheduled result was not admitted exactly once")
	}
	before := batch
	if repo, ok := batch.scheduleNext(repos); !ok || repo != repos[8] || batch.active != 8 || batch.next != 9 {
		t.Fatal("completion did not release exactly one replacement slot")
	}
	if before.inFlight[repos[8].ID] || before.next != 8 || before.active != 7 {
		t.Fatal("scheduling changed a prior batch copy")
	}
	batch.stop()
	if batch.admit(RepoLoadedMsg{Generation: 5, Repository: repos[0]}, time.Now()) {
		t.Fatal("stopped batch admitted a queued result")
	}
	if _, ok := batch.scheduleNext(repos); ok {
		t.Fatal("stopped batch scheduled another repository")
	}
	empty := newScanBatch(nil, 6, true)
	if snapshot, ready := empty.publish(localSnapshot{}); !ready || snapshot.loadError() != nil || len(snapshot.records) != 0 || empty.running {
		t.Fatal("empty startup batch did not publish a usable snapshot")
	}
}

func TestLocalScanBootLogPreservesEarlierModelCopy(t *testing.T) {
	m := NewModel([]string{"/a", "/b"}, stats.SortByTotal, nil, "", DefaultTimeIndex)
	t.Cleanup(m.Close)
	backing := []string{"earlier line", "unused capacity", "unused capacity"}
	m.bootLines = backing[:1]
	before := m
	next, _ := applyLocal(t, m, RepoLoadedMsg{Generation: m.scan.generation, Repository: m.repositories[0]})
	if len(next.bootLines) != 2 || len(before.bootLines) != 1 || backing[1] != "unused capacity" || len(before.scan.outcomes) != 0 {
		t.Fatal("accepted result changed an earlier model's boot log or pending state")
	}
}
