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
		{"Copilot <198982749+Copilot@users.noreply.github.com>", true},
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

	m := newAIMatcher(nil)
	for _, tc := range cases {
		got := m.isAICoAuthor(tc.input)
		if got != tc.want {
			t.Errorf("isAICoAuthor(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsAIGitHubBotAuthors(t *testing.T) {
	cases := []struct {
		email string
		want  bool
	}{
		{"198982749+Copilot@users.noreply.github.com", true},
		{"copilot@users.noreply.github.com", true},
		{"41898282+claude[bot]@users.noreply.github.com", true},
		{"158243242+devin-ai-integration[bot]@users.noreply.github.com", true},
		{"161369871+google-labs-jules[bot]@users.noreply.github.com", true},
		{"49699333+dependabot[bot]@users.noreply.github.com", false},
		{"12345+someone@users.noreply.github.com", false},
	}

	m := newAIMatcher(nil)
	for _, tc := range cases {
		if got := m.isAI(tc.email); got != tc.want {
			t.Errorf("isAI(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func TestAIMatcherCustomIdentities(t *testing.T) {
	m := newAIMatcher([]string{"Tag-Bot@TeamSense.com", "@agents.example.com", " ", ""})
	cases := []struct {
		value string
		want  bool
	}{
		{"Tag Bot <tag-bot@teamsense.com>", true},
		{"tag-bot@teamsense.com", true},
		{"Worker <w1@agents.example.com>", true},
		{"Rich Haase <rich@teamsense.com>", false},
		{"Claude <noreply@anthropic.com>", true},
	}
	for _, tc := range cases {
		if got := m.isAI(tc.value); got != tc.want {
			t.Errorf("custom isAI(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}

	if newAIMatcher(nil).isAI("tag-bot@teamsense.com") {
		t.Error("custom identity leaked into the default matcher")
	}
}
