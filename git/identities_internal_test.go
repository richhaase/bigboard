package git

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestIdentityResolverPreservesInputAndUnmappedIdentities(t *testing.T) {
	input := map[string]Identity{" CANONICAL@example.com ": {Aliases: []string{" ALIAS@example.com "}, Name: " Display Name "}}
	want := map[string]Identity{" CANONICAL@example.com ": {Aliases: []string{" ALIAS@example.com "}, Name: " Display Name "}}
	resolver, err := newIdentityResolver(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, want) {
		t.Fatal("identity normalization changed caller-owned configuration")
	}
	for _, email := range []string{" ALIAS@example.com ", "CANONICAL@example.com"} {
		name, canonical := resolver.resolve("Original", email)
		if name != "Display Name" || canonical != "canonical@example.com" {
			t.Fatalf("configured identity not resolved: %s <%s>", name, canonical)
		}
	}
	for _, email := range []string{"", "Unmapped@example.com"} {
		name, canonical := resolver.resolve(" Original ", email)
		if name != "Original" || canonical != email {
			t.Fatalf("unmapped identity changed: %s <%s>", name, canonical)
		}
	}
}

func TestAliasResolutionPrecedesAuthorAIClassification(t *testing.T) {
	for _, tc := range []struct {
		name, author, canonical, coauthor string
		wantAI                            bool
	}{
		{"alias becomes agent", "human@example.com", "noreply@anthropic.com", "", true},
		{"alias becomes human", "noreply@anthropic.com", "human@example.com", "", false},
		{"coauthor evidence retained", "noreply@anthropic.com", "human@example.com", "Claude <noreply@anthropic.com>", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := newIdentityResolver(map[string]Identity{tc.canonical: {Aliases: []string{tc.author}}})
			if err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(strings.NewReader(fmtHeader("Original Name", tc.author, "1767348000", tc.coauthor)))
			scanner.Split(splitNUL)
			records, err := parseLog(scanner, Repository{ID: "repo", Name: "repo"}, defaultPathFilter(CollectOptions{}), newAIMatcher(nil), resolver)
			if err != nil || len(records) != 1 {
				t.Fatalf("parse: %v, %+v", err, records)
			}
			if records[0].Email != tc.canonical || records[0].Author != "Original Name" || records[0].AIAssisted != tc.wantAI {
				t.Fatalf("wrong canonical author or AI evidence: %+v", records[0])
			}
		})
	}
}
