package stats

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/richhaase/bigboard/git"
)

func TestSubareasLiteralOneLevelAndEvidence(t *testing.T) {
	changes := []git.PathChange{{Path: "src/vs/workbench/a.go"}, {Path: "src/vs/editor/deep/b.go", PreviousPath: "src/vs/old/b.go"}, {Path: "src/vs/root.go"}}
	area := WorkArea{ID: "auto:src/vs", Name: "src/vs", Records: []git.CommitRecord{{CommitID: "a", Author: "A", Email: "a@example.com", Changes: changes}, {CommitID: "b", Changes: []git.PathChange{{Path: "src/vs/editor/direct.go"}}}}}
	got := Subareas(area, "src/vs")
	if len(got) != 3 {
		t.Fatalf("children=%+v", got)
	}
	counts := map[string]int{}
	for _, child := range got {
		counts[child.ID] = len(child.Records)
	}
	if !reflect.DeepEqual(counts, map[string]int{"direct:src/vs": 1, "path:src/vs/editor": 2, "path:src/vs/workbench": 1}) {
		t.Fatal(counts)
	}
	deep := Subareas(area, "src/vs/editor")
	if len(deep) != 2 || len(deep[0].Records[0].Changes) != 1 {
		t.Fatalf("nested=%+v", deep)
	}
	if !reflect.DeepEqual(area.Records[0].Changes, changes) {
		t.Fatal("mutated source")
	}
	for _, child := range got {
		for _, r := range child.Records {
			if r.CommitID == "a" && r.Email != "a@example.com" {
				t.Fatal("lost canonical identity")
			}
		}
	}
	if len(Subareas(area, "src/vs/old")) != 0 {
		t.Fatal("rename origin incorrectly counted as changed child")
	}
	if len(Subareas(area, "src/vscode")) != 0 {
		t.Fatal("prefix boundary escaped area")
	}
	area.ID = "named:Feature"
	if len(Subareas(area, "src/vs")) != 0 {
		t.Fatal("reinterpreted configured area")
	}
}

func TestSubareasSpecialAndDuplicateRecords(t *testing.T) {
	area := WorkArea{ID: "auto:src/vs", Records: []git.CommitRecord{{CommitID: "unknown", PathsUnknown: true}, {CommitID: "empty"}, {CommitID: "known", Changes: []git.PathChange{{Path: "src/vs/日本語/long.go"}}}, {CommitID: "known", Changes: []git.PathChange{{Path: "src/vs/日本語/long.go"}}}}}
	got := Subareas(area, "src/vs")
	if len(got) != 3 {
		t.Fatalf("children=%+v", got)
	}
	for _, child := range got {
		if len(child.Records) != 1 {
			t.Fatal("duplicate evidence", child)
		}
	}
}

func TestSubareasBusySampleKeepsEveryCommit(t *testing.T) {
	area := WorkArea{ID: "auto:src/vs"}
	for i := range 1200 {
		area.Records = append(area.Records, git.CommitRecord{CommitID: fmt.Sprintf("oid%d", i), Email: fmt.Sprintf("person%d@example.com", i%120), Changes: []git.PathChange{{Path: fmt.Sprintf("src/vs/child%02d/file%d.go", i%40, i)}, {Path: "src/vs/shared/index.go"}}})
	}
	children := Subareas(area, "src/vs")
	if len(children) != 41 {
		t.Fatal(len(children))
	}
	seen := map[string]bool{}
	for _, child := range children {
		for _, r := range child.Records {
			seen[r.CommitID] = true
		}
		if child.ID == "path:src/vs/shared" && len(child.Records) != 1200 {
			t.Fatal("large overlapping child lost evidence")
		}
	}
	if len(seen) != 1200 {
		t.Fatal("lost commits", len(seen))
	}
}
