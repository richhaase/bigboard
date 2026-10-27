package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

func TestRecordSelectionPreservesLegacyRepositoryAndEvidenceMatching(t *testing.T) {
	api := git.CommitRecord{RepoName: "api", Author: "Missing", Date: time.Now()}
	web := git.CommitRecord{RepoName: "web", Author: "Missing", Date: api.Date}
	records := []git.CommitRecord{api, web, api}
	m := awarenessFixture()
	m.local.records = records
	if got := m.recordsInRepository(git.Repository{Name: "api"}, records); !reflect.DeepEqual(got, records) {
		t.Fatalf("ID-less repository compatibility changed: %#v", got)
	}
	if got := m.recordsInRepository(git.Repository{ID: "/api", Name: "api"}, records); !reflect.DeepEqual(got, []git.CommitRecord{api, api}) {
		t.Fatalf("legacy-name fallback lost distinct records: %#v", got)
	}
	if got := m.evidence("api", ""); !reflect.DeepEqual(got, []git.CommitRecord{api, api}) {
		t.Fatalf("name-scoped evidence broadened or merged missing IDs: %#v", got)
	}
}
