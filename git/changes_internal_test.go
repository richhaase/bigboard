package git

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestRetainedChangesLiteralPaths(t *testing.T) {
	out := fmtHeader("Dev", "dev@example.com", "2026-01-02T10:00:00Z") +
		"\n2\t1\t\nleading\tline.go\x00" +
		"-\t-\timage\twith\nlines.png\x00" +
		"9\t0\tvendor/generated\tfile.go\x00" +
		"0\t0\t\x00old\tname.go\x00new\nname.go\x00" +
		"-\t-\t\x00old\nbinary.png\x00vendor/copied\tbinary.png\x00" +
		fmtHeader("Empty", "empty@example.com", "2026-01-01T10:00:00Z")
	want := []PathChange{
		{Path: "\nleading\tline.go"},
		{Path: "image\twith\nlines.png"},
		{Path: "vendor/generated\tfile.go", Generated: true},
		{Path: "new\nname.go", PreviousPath: "old\tname.go"},
		{Path: "vendor/copied\tbinary.png", PreviousPath: "old\nbinary.png", Generated: true},
	}
	for _, includeGenerated := range []bool{false, true} {
		scanner := bufio.NewScanner(strings.NewReader(out))
		scanner.Split(splitNUL)
		records, err := parseLog(scanner, Repository{ID: "repo", Name: "repo"}, defaultPathFilter(CollectOptions{IncludeGenerated: includeGenerated}), newAIMatcher(nil))
		if err != nil {
			t.Fatal(err)
		}
		if includeGenerated {
			for i := range want {
				want[i].Generated = false
			}
		}
		if len(records) != 2 || !reflect.DeepEqual(records[0].Changes, want) {
			t.Fatalf("literal changes (IncludeGenerated=%v): %+v", includeGenerated, records)
		}
		if len(records[1].Changes) != 0 || records[1].PathsUnknown || records[0].PathsUnknown {
			t.Fatalf("known empty commit confused with unknown: %+v", records)
		}
		wantAdded := 2
		if includeGenerated {
			wantAdded += 9
		}
		if records[0].Added != wantAdded || records[0].Removed != 1 {
			t.Fatalf("retained paths changed line counts: %+v", records[0])
		}
	}
}
