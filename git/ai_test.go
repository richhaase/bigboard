package git

import "testing"

func TestIsAICoAuthor(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"Claude <noreply@anthropic.com>", true},
		{"Claude Opus 4.6 (1M context) <noreply@anthropic.com>", true},
		{"Claude Sonnet 4.6 <noreply@anthropic.com>", true},
		{"GitHub Copilot <copilot@github.com>", true},
		{"Cursor <hi@cursor.com>", true},
		{"Cursor <hi@cursor.sh>", true},
		{"Devin <devin@cognition.ai>", true},
		{"Rich Haase <rich@example.com>", false},
		{"", false},
		{"  ", false},
		{"noreply@anthropic.com", true},
		{"Eve <person@openai.com.evil.example>", false},
		{"Not Copilot <notcopilot@github.com>", false},
	}

	for _, tc := range cases {
		got := isAICoAuthor(tc.input)
		if got != tc.want {
			t.Errorf("isAICoAuthor(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
