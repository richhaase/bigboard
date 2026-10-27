package stats_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestUniqueRecordsWithMatchingCopyKeepsMembershipAndMissingIDs(t *testing.T) {
	shallow := git.CommitRecord{CommitID: "shared", RepoID: "a", Author: "Alias", Email: "alias@test", LinesUnknown: true}
	full := git.CommitRecord{CommitID: "shared", RepoID: "b", Author: "Canonical", Email: "canonical@test", Added: 7, Changes: []git.PathChange{{Path: "src/main.go", Added: 7, LinesKnown: true}}}
	missing := git.CommitRecord{RepoID: "a", Author: "Missing", Added: 2}
	unrelated := git.CommitRecord{CommitID: "unrelated", RepoID: "b", Author: "Other"}
	records := []git.CommitRecord{shallow, missing, unrelated, full, missing, {RepoID: "b", Author: "Missing"}}
	var inspected []git.CommitRecord
	got := stats.UniqueRecordsWithMatchingCopy(records, func(record git.CommitRecord) bool {
		inspected = append(inspected, record)
		return record.RepoID == "a"
	})
	if want := []git.CommitRecord{missing, missing, full}; !reflect.DeepEqual(got, want) {
		t.Fatalf("membership or legacy ordering changed: got %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(inspected, records) {
		t.Fatal("membership was not evaluated on each original copy in order")
	}
	if got := stats.UniqueRecordsWithMatchingCopy(records, func(record git.CommitRecord) bool { return record.RepoID == "absent" }); len(got) != 0 {
		t.Fatalf("absent repository inherited records: %#v", got)
	}
}

func TestCanonicalSelectionUsesOnlyFilteredCopies(t *testing.T) {
	now := time.Now()
	full := git.CommitRecord{CommitID: "shared", RepoID: "a", RepoName: "full", Author: "Canonical", Email: "canonical@test", Added: 7, Date: now.Add(-72 * time.Hour)}
	shallow := git.CommitRecord{CommitID: "shared", RepoID: "b", RepoName: "shallow", Author: "Alias", Email: "alias@test", LinesUnknown: true, Date: now}
	records := []git.CommitRecord{full, shallow}
	matches := func(record git.CommitRecord) bool { return record.RepoID == "b" }
	if got := stats.UniqueRecordsWithMatchingCopy(records, matches); !reflect.DeepEqual(got, []git.CommitRecord{full}) {
		t.Fatalf("complete copy did not supply selected repository evidence: %#v", got)
	}
	for name, filtered := range map[string][]git.CommitRecord{
		"repository": stats.FilterByRepo(records, map[string]bool{"a": true}),
		"time":       stats.FilterByTime(records, 24*time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			if got := stats.UniqueRecordsWithMatchingCopy(filtered, matches); !reflect.DeepEqual(got, []git.CommitRecord{shallow}) {
				t.Fatalf("filtered-out canonical copy survived: %#v", got)
			}
			if got := stats.FilterByCanonicalIdentity(filtered, map[string]bool{stats.IdentityID(shallow): true}); !reflect.DeepEqual(got, []git.CommitRecord{shallow}) {
				t.Fatalf("identity was chosen before filtering: %#v", got)
			}
		})
	}
}

func TestFilterByCanonicalIdentityRetainsOriginalCopiesAndOrder(t *testing.T) {
	a := git.CommitRecord{CommitID: "shared", RepoID: "a", Author: "Canonical", Email: "canonical@test"}
	b := git.CommitRecord{CommitID: "shared", RepoID: "b", Author: "Alias", Email: "alias@test"}
	missingA := git.CommitRecord{RepoID: "a", Author: "Missing"}
	missingB := git.CommitRecord{RepoID: "b", Author: "Missing"}
	records := []git.CommitRecord{b, missingB, a, missingA, a, missingA}
	for _, test := range []struct {
		name       string
		identities map[string]bool
		want       []git.CommitRecord
	}{
		{"canonical", map[string]bool{stats.IdentityID(a): true}, []git.CommitRecord{b, a, a}},
		{"noncanonical", map[string]bool{stats.IdentityID(b): true}, nil},
		{"missing email and ID", map[string]bool{stats.IdentityID(missingA): true}, []git.CommitRecord{missingA, missingA}},
		{"false selection", map[string]bool{stats.IdentityID(a): false}, nil},
		{"empty selection", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := stats.FilterByCanonicalIdentity(records, test.identities); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("original copies or identities changed: got %#v, want %#v", got, test.want)
			}
		})
	}
	if want := []git.CommitRecord{b, missingB, a, missingA, a, missingA}; !reflect.DeepEqual(records, want) {
		t.Fatal("filtering changed the input records")
	}
}

func TestRecordSelectionReusesCanonicalCopiesAcrossMemberships(t *testing.T) {
	missing := git.CommitRecord{RepoID: "a", Author: "Missing", Added: 2}
	shallow := git.CommitRecord{CommitID: "shared", RepoID: "a", Author: "Alias", Email: "alias@test", LinesUnknown: true}
	full := git.CommitRecord{CommitID: "shared", RepoID: "b", Author: "Canonical", Email: "canonical@test", Added: 7}
	records := []git.CommitRecord{shallow, missing, full, missing}
	selection := stats.NewRecordSelection(records)
	for _, test := range []struct {
		repo string
		want []git.CommitRecord
	}{
		{"a", []git.CommitRecord{missing, missing, full}},
		{"b", []git.CommitRecord{full}},
		{"absent", nil},
		{"a", []git.CommitRecord{missing, missing, full}},
	} {
		var inspected []git.CommitRecord
		got := selection.MatchingCopy(func(record git.CommitRecord) bool {
			inspected = append(inspected, record)
			return record.RepoID == test.repo
		})
		if !reflect.DeepEqual(got, test.want) || !reflect.DeepEqual(inspected, records) {
			t.Fatalf("selection for %s changed membership, ordering or attribution: %#v", test.repo, got)
		}
	}
	filtered := stats.NewRecordSelection(stats.FilterByRepo(records, map[string]bool{"b": true}))
	got := filtered.MatchingCopy(func(record git.CommitRecord) bool { return record.RepoID == "a" })
	if !reflect.DeepEqual(got, []git.CommitRecord{missing, missing, shallow}) {
		t.Fatalf("selection reused a filtered-out canonical copy: %#v", got)
	}
}
