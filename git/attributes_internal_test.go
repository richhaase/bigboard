package git

import "testing"

func TestAttributeRules(t *testing.T) {
	rules := parseAttributes(`# comment
*.json linguist-generated
seeds/**/*.json linguist-generated=true
/live/registry/*.json linguist-vendored
config/*.json -linguist-generated
keep.json linguist-generated=false
docs/ linguist-generated
!negated linguist-generated
*.go diff
odd.json linguist-generated=maybe
`)
	cases := []struct {
		path string
		want string
	}{
		{"data.json", "true"},
		{"nested/deep/data.json", "true"},
		{"seeds/a/b/payload.json", "true"},
		{"live/registry/security.json", "true"},
		{"other/live/registry/security.json", "true"},
		{"config/app.json", "false"},
		{"config/sub/app.json", "true"},
		{"nested/keep.json", "false"},
		{"docs/readme.md", "unset"},
		{"main.go", "unset"},
	}
	for _, tc := range cases {
		got := "unset"
		if value := attributeGenerated(rules, tc.path); value != nil {
			got = map[bool]string{true: "true", false: "false"}[*value]
		}
		if got != tc.want {
			t.Errorf("%s: got %s want %s", tc.path, got, tc.want)
		}
	}
}

func TestExclusionReasons(t *testing.T) {
	filter := defaultPathFilter(CollectOptions{})
	filter.attributes = parseAttributes("generated/** linguist-generated\nvendor/keep.go linguist-vendored=false\nlocked.json -linguist-generated\n")
	cases := []struct {
		path           string
		added, removed int
		known          bool
		want           string
	}{
		{"src/main.go", 5000, 0, true, ""},
		{"data/registry.json", 1999, 0, true, ""},
		{"data/registry.json", 1000, 1000, true, ExclusionBulk},
		{"locale/fr.PO", 2500, 0, true, ExclusionBulk},
		{"data/registry.json", 0, 0, false, ""},
		{"locked.json", 9000, 0, true, ""},
		{"generated/api.go", 3, 0, true, ExclusionAttribute},
		{"vendor/keep.go", 3, 0, true, ""},
		{"vendor/lib.go", 3, 0, true, ExclusionPath},
		{"package-lock.json", 3, 0, true, ExclusionPath},
	}
	for _, tc := range cases {
		if got := filter.exclusion(tc.path, tc.added, tc.removed, tc.known); got != tc.want {
			t.Errorf("%s %d/%d: got %q want %q", tc.path, tc.added, tc.removed, got, tc.want)
		}
	}
	filter.includeGenerated = true
	if got := filter.exclusion("generated/api.go", 9000, 0, true); got != "" {
		t.Errorf("IncludeGenerated must count every path, got %q", got)
	}
}
