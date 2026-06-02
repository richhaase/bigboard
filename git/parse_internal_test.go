package git

import (
	"fmt"
	"strings"
	"testing"
)

// fmtHeader builds a git-log header line in the format CollectCommits requests:
// fields separated by 0x1e, co-authors within the trailer field by 0x1f.
func fmtHeader(name, email, date string, coAuthors ...string) string {
	return strings.Join([]string{name, email, date, strings.Join(coAuthors, "\x1f")}, fieldSep)
}

func TestParseGitLogBasic(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Alice", "alice@example.com", "2026-01-02T10:00:00Z"),
		"10\t5\ta.go",
		"3\t0\tb.go",
		"",
		fmtHeader("Bob", "bob@example.com", "2026-01-01T10:00:00Z"),
		"1\t1\tc.go",
	}, "\n")

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Author != "Alice" || records[0].Added != 13 || records[0].Removed != 5 || records[0].Files != 2 {
		t.Errorf("Alice record wrong: %+v", records[0])
	}
	if records[1].Author != "Bob" || records[1].Added != 1 || records[1].Removed != 1 {
		t.Errorf("Bob record wrong: %+v", records[1])
	}
}

// TestParseGitLogPipeInName is the regression guard for the separator fix: an
// author name containing '|' must no longer drop the commit or misattribute its
// line counts to the previous commit.
func TestParseGitLogPipeInName(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Good Dev", "good@example.com", "2026-01-02T10:00:00Z"),
		"100\t0\ta.go",
		"",
		fmtHeader("Bad|Name", "bad@example.com", "2026-01-01T10:00:00Z"),
		"50\t0\tb.go",
	}, "\n")

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	var pipe *CommitRecord
	for i := range records {
		if records[i].Author == "Bad|Name" {
			pipe = &records[i]
		}
	}
	if pipe == nil {
		t.Fatal("commit with '|' in author name was dropped")
	}
	if pipe.Added != 50 {
		t.Errorf("pipe-name commit lines misattributed: Added=%d, want 50", pipe.Added)
	}
	// The previous commit must NOT have absorbed the pipe commit's lines.
	for i := range records {
		if records[i].Author == "Good Dev" && records[i].Added != 100 {
			t.Errorf("Good Dev commit corrupted: Added=%d, want 100", records[i].Added)
		}
	}
}

func TestParseGitLogBinaryAndMalformed(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Alice", "alice@example.com", "2026-01-02T10:00:00Z"),
		"-\t-\timage.png", // binary: skipped
		"7\t2\tcode.go",
		"",
		// Malformed date — whole commit should be dropped, not crash.
		fmtHeader("Ghost", "ghost@example.com", "not-a-date"),
		"99\t99\tx.go",
	}, "\n")

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (binary skipped, malformed dropped), got %d", len(records))
	}
	if records[0].Added != 7 || records[0].Removed != 2 || records[0].Files != 1 {
		t.Errorf("binary line not skipped correctly: %+v", records[0])
	}
}

func TestParseGitLogAIDetection(t *testing.T) {
	cases := []struct {
		name   string
		header string
		wantAI bool
	}{
		{"human", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z"), false},
		{"ai-as-author", fmtHeader("Claude", "noreply@anthropic.com", "2026-01-01T10:00:00Z"), true},
		{"ai-co-author", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Claude <noreply@anthropic.com>"), true},
		{"multi-co-author-with-ai", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Pat <pat@example.com>", "Claude <noreply@anthropic.com>"), true},
		{"multi-co-author-no-ai", fmtHeader("Human", "human@example.com", "2026-01-01T10:00:00Z", "Pat <pat@example.com>", "Sam <sam@example.com>"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.header + "\n5\t0\tfile.go"
			records, err := parseGitLog(out, "repo")
			if err != nil {
				t.Fatalf("parseGitLog: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(records))
			}
			if records[0].AIAssisted != tc.wantAI {
				t.Errorf("AIAssisted = %v, want %v (header %q)", records[0].AIAssisted, tc.wantAI, fmt.Sprintf("%q", tc.header))
			}
		})
	}
}

func TestParseGitLogPathFiltering(t *testing.T) {
	out := strings.Join([]string{
		fmtHeader("Dev", "dev@example.com", "2026-01-02T10:00:00Z"),
		"20\t1\tsrc/app.go",            // counted
		"5000\t0\tpackage-lock.json",   // ignored (basename glob)
		"800\t0\tvendor/lib/x.go",      // ignored (dir segment)
		"3\t0\tsrc/{old.go => new.go}", // counted (rename, resolves to src/new.go)
	}, "\n")

	records, err := parseGitLog(out, "repo")
	if err != nil {
		t.Fatalf("parseGitLog: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Added != 23 { // 20 + 3, excluding the 5800 generated lines
		t.Errorf("Added = %d, want 23 (generated/vendored excluded)", records[0].Added)
	}
	if records[0].Files != 2 {
		t.Errorf("Files = %d, want 2 (only counted files)", records[0].Files)
	}
}

func TestEffectivePath(t *testing.T) {
	cases := map[string]string{
		"src/app.go":               "src/app.go",
		"old.go => new.go":         "new.go",
		"{old.go => new.go}":       "new.go",
		"src/{old => new}/file.go": "src/new/file.go",
	}
	for in, want := range cases {
		if got := effectivePath(in); got != want {
			t.Errorf("effectivePath(%q) = %q, want %q", in, got, want)
		}
	}
}
