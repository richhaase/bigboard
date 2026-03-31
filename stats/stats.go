package stats

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/rdh/bigboard/git"
)

// SortField controls which metric is used for sorting AuthorStats.
type SortField int

const (
	SortByTotal SortField = iota
	SortByCommits
	SortByAdded
	SortByRemoved
	SortByNet
)

// AuthorStats holds aggregated contribution data for a single author.
type AuthorStats struct {
	Name        string
	Commits     int
	Added       int
	Removed     int
	Net         int
	TotalChange int
	PerRepo     map[string]*RepoContribution
}

// RepoContribution holds per-repository stats for an author.
type RepoContribution struct {
	Commits     int
	Added       int
	Removed     int
	Net         int
	TotalChange int
}

// FilterByTime returns records within d from now. d == 0 returns all records.
func FilterByTime(records []git.CommitRecord, d time.Duration) []git.CommitRecord {
	if d == 0 {
		return records
	}
	cutoff := time.Now().Add(-d)
	out := make([]git.CommitRecord, 0, len(records))
	for _, r := range records {
		if r.Date.After(cutoff) {
			out = append(out, r)
		}
	}
	return out
}

// FilterByRepo returns records not in the excluded set. Keys are repo names.
func FilterByRepo(records []git.CommitRecord, excluded map[string]bool) []git.CommitRecord {
	if len(excluded) == 0 {
		return records
	}
	out := make([]git.CommitRecord, 0, len(records))
	for _, r := range records {
		if !excluded[r.RepoName] {
			out = append(out, r)
		}
	}
	return out
}

// Aggregate groups records by author and computes totals.
// Uses a two-pass merge: first group by email, then fuzzy-match remaining names.
func Aggregate(records []git.CommitRecord) []AuthorStats {
	// Pass 1: build email→canonical name map.
	// For each email, pick the name with the most commits.
	type nameCount struct {
		name  string
		count int
	}
	emailNames := make(map[string]*nameCount)
	for _, r := range records {
		email := strings.ToLower(r.Email)
		if email == "" {
			continue
		}
		nc, ok := emailNames[email]
		if !ok {
			emailNames[email] = &nameCount{name: r.Author, count: 1}
		} else {
			nc.count++
			// Keep the longer/more complete name variant
			if len(r.Author) > len(nc.name) {
				nc.name = r.Author
			}
		}
	}

	// Build email→canonical name lookup, and group emails that share a canonical name
	emailToCanonical := make(map[string]string)
	for email, nc := range emailNames {
		emailToCanonical[email] = nc.name
	}

	// Pass 2: map each record's author to a canonical name.
	// First try email grouping, then fall back to fuzzy name matching.
	canonicalForAuthor := make(map[string]string)

	// Collect all unique (author, email) pairs
	type authorEmail struct {
		author string
		email  string
	}
	seen := make(map[authorEmail]bool)
	var pairs []authorEmail
	for _, r := range records {
		ae := authorEmail{r.Author, strings.ToLower(r.Email)}
		if !seen[ae] {
			seen[ae] = true
			pairs = append(pairs, ae)
		}
	}

	// Group by email first: all authors sharing an email get the same canonical name
	emailGroupCanonical := make(map[string]string) // email → chosen canonical
	for _, ae := range pairs {
		if ae.email == "" {
			continue
		}
		if _, ok := emailGroupCanonical[ae.email]; !ok {
			emailGroupCanonical[ae.email] = emailToCanonical[ae.email]
		}
		canonicalForAuthor[ae.author] = emailGroupCanonical[ae.email]
	}

	// For authors without an email match, try fuzzy name matching against known canonicals
	allCanonicals := make(map[string]bool)
	for _, c := range canonicalForAuthor {
		allCanonicals[c] = true
	}

	for _, ae := range pairs {
		if _, ok := canonicalForAuthor[ae.author]; ok {
			continue
		}
		// Try fuzzy match against existing canonical names
		for c := range allCanonicals {
			if AreSimilarNames(ae.author, c) {
				canonicalForAuthor[ae.author] = c
				break
			}
		}
		if _, ok := canonicalForAuthor[ae.author]; !ok {
			canonicalForAuthor[ae.author] = ae.author
			allCanonicals[ae.author] = true
		}
	}

	// Final pass: fuzzy-merge canonical names themselves
	canonicalList := make([]string, 0, len(allCanonicals))
	for c := range allCanonicals {
		canonicalList = append(canonicalList, c)
	}
	commitCounts := make(map[string]int)
	for _, r := range records {
		c := canonicalForAuthor[r.Author]
		commitCounts[c]++
	}
	mergedCanonical := buildCanonicalMap(canonicalList, commitCounts)

	// Aggregate
	byName := make(map[string]*AuthorStats)
	for _, r := range records {
		name := mergedCanonical[canonicalForAuthor[r.Author]]
		as, ok := byName[name]
		if !ok {
			as = &AuthorStats{
				Name:    name,
				PerRepo: make(map[string]*RepoContribution),
			}
			byName[name] = as
		}
		as.Commits++
		as.Added += r.Added
		as.Removed += r.Removed
		as.Net += r.Added - r.Removed
		as.TotalChange += r.Added + r.Removed

		rc, ok := as.PerRepo[r.RepoName]
		if !ok {
			rc = &RepoContribution{}
			as.PerRepo[r.RepoName] = rc
		}
		rc.Commits++
		rc.Added += r.Added
		rc.Removed += r.Removed
		rc.Net += r.Added - r.Removed
		rc.TotalChange += r.Added + r.Removed
	}

	result := make([]AuthorStats, 0, len(byName))
	for _, as := range byName {
		result = append(result, *as)
	}
	return result
}

// Sort sorts stats descending by the given field.
func Sort(stats []AuthorStats, field SortField) {
	sort.SliceStable(stats, func(i, j int) bool {
		switch field {
		case SortByCommits:
			return stats[i].Commits > stats[j].Commits
		case SortByAdded:
			return stats[i].Added > stats[j].Added
		case SortByRemoved:
			return stats[i].Removed > stats[j].Removed
		case SortByNet:
			return stats[i].Net > stats[j].Net
		default: // SortByTotal
			return stats[i].TotalChange > stats[j].TotalChange
		}
	})
}

// SortFieldFromString converts a string to a SortField. Defaults to SortByTotal.
func SortFieldFromString(s string) SortField {
	switch strings.ToLower(s) {
	case "commits":
		return SortByCommits
	case "added":
		return SortByAdded
	case "removed":
		return SortByRemoved
	case "net":
		return SortByNet
	default:
		return SortByTotal
	}
}

// AreSimilarNames returns true if names match case-insensitively with whitespace
// normalization, or if one is a substring of the other (only for names longer than 5 chars).
func AreSimilarNames(a, b string) bool {
	na := normalizedName(a)
	nb := normalizedName(b)
	if na == nb {
		return true
	}
	if len(na) > 5 && strings.Contains(nb, na) {
		return true
	}
	if len(nb) > 5 && strings.Contains(na, nb) {
		return true
	}
	return false
}

// MergeAuthorName returns the canonical name for the given name from allNames,
// choosing the name with the highest commit count among similar names.
func MergeAuthorName(name string, allNames []string, commitCounts map[string]int) string {
	best := name
	bestCount := commitCounts[name]

	for _, candidate := range allNames {
		if candidate == name {
			continue
		}
		if AreSimilarNames(name, candidate) {
			count := commitCounts[candidate]
			if count > bestCount {
				bestCount = count
				best = candidate
			}
		}
	}
	return best
}

// buildCanonicalMap builds a map from each raw author name to its canonical name.
func buildCanonicalMap(allNames []string, commitCounts map[string]int) map[string]string {
	canonical := make(map[string]string, len(allNames))
	for _, name := range allNames {
		canonical[name] = MergeAuthorName(name, allNames, commitCounts)
	}
	return canonical
}

// normalizedName lowercases and strips all whitespace, hyphens, underscores,
// and dots so "Mario Payan", "MarioPayan", "mario.payan" all become "mariopayan".
func normalizedName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
