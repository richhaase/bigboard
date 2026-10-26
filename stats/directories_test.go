package stats

import (
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
)

func directoryEntriesByID(entries []DirectoryEntry) map[string]DirectoryEntry {
	result := make(map[string]DirectoryEntry)
	for _, entry := range entries {
		result[entry.ID] = entry
	}
	return result
}

func TestDirectoryChildrenUseFileCountsAtEveryLevel(t *testing.T) {
	date := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	records := []git.CommitRecord{
		{CommitID: "cross-directory", Author: "Contributor", Email: "contributor@example.test", Date: date, RepoID: "/repo", RepoName: "repo", Subject: "Change several paths", AIAssisted: true, Added: 112, Removed: 23, Changes: []git.PathChange{
			{Path: "src/main.go", Added: 10, Removed: 2, LinesKnown: true},
			{Path: "tests/main_test.go", Added: 100, Removed: 20, LinesKnown: true},
			{Path: "README.md", Added: 2, Removed: 1, LinesKnown: true},
		}},
		{CommitID: "nested", Email: "contributor@example.test", Date: date, Added: 6, Removed: 3, Changes: []git.PathChange{
			{Path: "src/nested/file.go", Added: 6, Removed: 3, LinesKnown: true},
		}},
	}
	root := directoryEntriesByID(DirectoryChildren(records, ""))
	if len(root) != 3 || !root["dir:src"].IsDir || root["file:README.md"].IsDir || root["dir:src"].Name != "src" {
		t.Fatalf("wrong immediate root children: %+v", root)
	}
	if len(root["dir:src"].Records) != 2 || len(root["dir:tests"].Records) != 1 {
		t.Fatal("cross-directory commits were lost or duplicated within a directory")
	}
	for id, want := range map[string][2]int{"dir:src": {16, 5}, "dir:tests": {100, 20}, "file:README.md": {2, 1}} {
		var added, removed int
		for _, record := range root[id].Records {
			added += record.Added
			removed += record.Removed
		}
		if added != want[0] || removed != want[1] {
			t.Fatalf("%s received whole-commit totals: +%d -%d", id, added, removed)
		}
	}
	scoped := DirectoryRecords(records, "dir:src")
	if !reflect.DeepEqual(scoped, root["dir:src"].Records) {
		t.Fatal("directory evidence differs from directory row totals")
	}
	first := scoped[0]
	if first.CommitID != records[0].CommitID || first.Author != records[0].Author || first.Email != records[0].Email || first.Date != date || first.RepoID != "/repo" || first.Subject != records[0].Subject || !first.AIAssisted {
		t.Fatal("scoping changed commit identity or metadata")
	}
	children := directoryEntriesByID(DirectoryChildren(scoped, "src"))
	if len(children) != 2 || children["file:src/main.go"].Records[0].Added != 10 || children["dir:src/nested"].Records[0].Added != 6 {
		t.Fatal("child line counts changed after parent scoping")
	}
	leaf := DirectoryRecords(records, "file:src/nested/file.go")
	if len(leaf) != 1 || leaf[0].Added != 6 || leaf[0].Removed != 3 || len(leaf[0].Changes) != 1 {
		t.Fatalf("wrong file detail: %+v", leaf)
	}
	if len(DirectoryChildren(records, "sr")) != 0 || len(DirectoryRecords(records, "dir:sr")) != 0 || len(DirectoryRecords(records, "file:src/main")) != 0 {
		t.Fatal("directory or filename prefix matched a neighboring scope")
	}
	scoped[0].Changes[0].Path = "mutated"
	if records[0].Changes[0].Path != "src/main.go" || records[0].Added != 112 || records[0].Removed != 23 || len(records[0].Changes) != 3 {
		t.Fatal("directory scoping mutated original records")
	}
}

func TestDirectoriesExcludeGeneratedAndKeepBinaryRenameEvidence(t *testing.T) {
	records := []git.CommitRecord{
		{CommitID: "mixed", Added: 7, Removed: 2, Changes: []git.PathChange{
			{Path: "src/new.go", PreviousPath: "old/previous.go", Added: 7, Removed: 2, LinesKnown: true},
			{Path: "assets/logo.bin", Binary: true},
			{Path: "vendor/generated.go", Generated: true, Added: 9000, Removed: 8000, LinesKnown: true},
		}},
		{CommitID: "generated-only", Changes: []git.PathChange{{Path: "vendor/only.go", Generated: true, Added: 100, LinesKnown: true}}},
		{CommitID: "empty"},
	}
	root := directoryEntriesByID(DirectoryChildren(records, ""))
	if len(root) != 3 || len(root["dir:src"].Records) != 1 || len(root["dir:assets"].Records) != 1 || len(root["empty"].Records) != 2 {
		t.Fatalf("incorrect generated/binary groups: %+v", root)
	}
	if !root["empty"].Empty || root["empty"].Name != "No counted file changes" {
		t.Fatal("uncounted commits lack a clear scope label")
	}
	if len(DirectoryChildren(records, "old")) != 0 || len(DirectoryRecords(records, "dir:old")) != 0 || len(DirectoryRecords(records, "dir:vendor")) != 0 {
		t.Fatal("rename origin or generated files became a changed directory")
	}
	binary := DirectoryRecords(records, "file:assets/logo.bin")
	if len(binary) != 1 || binary[0].Added != 0 || binary[0].Removed != 0 || binary[0].LinesUnknown || !binary[0].Changes[0].Binary {
		t.Fatalf("binary file invented numeric or missing line data: %+v", binary)
	}
	renamed := DirectoryRecords(records, "file:src/new.go")
	if len(renamed) != 1 || renamed[0].Added != 7 || renamed[0].Removed != 2 || renamed[0].Changes[0].PreviousPath != "old/previous.go" {
		t.Fatal("rename evidence or exact line counts were lost")
	}
	if !reflect.DeepEqual(DirectoryRecords(records, "empty"), root["empty"].Records) {
		t.Fatal("empty-scope evidence differs from displayed commits")
	}
}

func TestDirectoriesPreserveUnknownPathsAndLineQualifiers(t *testing.T) {
	records := []git.CommitRecord{
		{CommitID: "unknown-paths", PathsUnknown: true, Added: 999, Changes: []git.PathChange{{Path: "invented/file.go", Added: 999, LinesKnown: true}}},
		{CommitID: "unknown-lines", LinesUnknown: true, Changes: []git.PathChange{{Path: "src/unknown.go", Added: 50, LinesKnown: true}}},
		{CommitID: "unmeasured-path", Changes: []git.PathChange{{Path: "src/unmeasured.go"}}},
		{CommitID: "known-zero", Changes: []git.PathChange{{Path: "src/zero.go", LinesKnown: true}}},
	}
	root := directoryEntriesByID(DirectoryChildren(records, ""))
	unknown := root["unknown"]
	if len(root) != 2 || !unknown.Unknown || len(unknown.Records) != 1 || !unknown.Records[0].LinesUnknown || unknown.Records[0].Added != 0 || len(unknown.Records[0].Changes) != 0 {
		t.Fatalf("unknown paths became attributable data: %+v", root)
	}
	for _, id := range []string{"file:src/unknown.go", "file:src/unmeasured.go"} {
		scoped := DirectoryRecords(records, id)
		if len(scoped) != 1 || !scoped[0].LinesUnknown || scoped[0].Added != 0 || scoped[0].Removed != 0 {
			t.Fatalf("%s lost unknown-line evidence: %+v", id, scoped)
		}
	}
	zero := DirectoryRecords(records, "file:src/zero.go")
	if len(zero) != 1 || zero[0].LinesUnknown || zero[0].Added != 0 {
		t.Fatal("measured zero was confused with missing line data")
	}
	children := directoryEntriesByID(DirectoryChildren(records, "src"))
	if len(children) != 3 || children["unknown"].Unknown || len(DirectoryChildren(records, "missing")) != 0 {
		t.Fatal("unattributed paths or empty commits appeared beneath a directory")
	}
	if !reflect.DeepEqual(DirectoryRecords(records, "unknown"), unknown.Records) {
		t.Fatal("unknown-path detail does not match the root evidence")
	}
}

func TestDirectoriesUseCanonicalUniqueRecords(t *testing.T) {
	known := git.CommitRecord{CommitID: "same-object", RepoID: "/known", Author: "Known", Added: 7, Changes: []git.PathChange{{Path: "src/file.go", Added: 7, LinesKnown: true}}}
	unknown := known
	unknown.RepoID, unknown.LinesUnknown, unknown.PathsUnknown, unknown.Changes = "/unknown", true, true, nil
	withoutID := known
	withoutID.CommitID = ""
	records := []git.CommitRecord{unknown, known, known, withoutID, withoutID}
	children := DirectoryChildren(records, "")
	if len(children) != 1 || len(children[0].Records) != 3 {
		t.Fatalf("canonical deduplication changed: %+v", children)
	}
	for _, record := range children[0].Records {
		if record.LinesUnknown || record.Added != 7 || record.Author != "Known" {
			t.Fatal("incomplete copy replaced complete line evidence")
		}
	}
	if len(DirectoryRecords(records, "dir:src")) != 3 {
		t.Fatal("scope details use different commit deduplication")
	}
}
