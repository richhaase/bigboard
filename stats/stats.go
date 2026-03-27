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

// Aggregate groups records by author (with fuzzy name merging) and computes totals.
func Aggregate(records []git.CommitRecord) []AuthorStats {
	// Count commits per raw author name to determine canonical names.
	commitCounts := make(map[string]int)
	for _, r := range records {
		commitCounts[r.Author]++
	}

	allNames := make([]string, 0, len(commitCounts))
	for name := range commitCounts {
		allNames = append(allNames, name)
	}

	canonical := buildCanonicalMap(allNames, commitCounts)

	// Aggregate by canonical name.
	byName := make(map[string]*AuthorStats)

	for _, r := range records {
		name := canonical[r.Author]
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

// normalizedName lowercases and collapses whitespace.
func normalizedName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}
