package stats

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/richhaase/bigboard/git"
)

// SortField controls which metric is used for sorting AuthorStats.
type SortField int

const (
	SortByTotal SortField = iota
	SortByCommits
	SortByAdded
	SortByRemoved
	SortByNet
	SortByAI
)

// numSortFields is the number of SortField values, used for cycling.
const numSortFields = 6

// FuzzyMatching enables substring-based name merging across different emails
// (e.g. "Christopher" into "Christopher Lee"). It is OFF by default because it
// over-merges genuinely distinct people (Daniel/Daniela, Martin/Martinez).
// Enable via the --fuzzy flag. With it off, identities merge only when they
// share an email or have an exactly-equal normalized name.
var FuzzyMatching = false

// AuthorStats holds aggregated contribution data for a single author.
type AuthorStats struct {
	Name        string                       `json:"name"`
	Commits     int                          `json:"commits"`
	Added       int                          `json:"added"`
	Removed     int                          `json:"removed"`
	Net         int                          `json:"net"`
	TotalChange int                          `json:"total_change"`
	AICommits   int                          `json:"ai_commits"`
	FirstCommit time.Time                    `json:"first_commit"`
	LastCommit  time.Time                    `json:"last_commit"`
	ActiveDays  int                          `json:"active_days"`
	PerRepo     map[string]*RepoContribution `json:"per_repo,omitempty"`
	// Aliases is the set of raw author-name spellings that merged into this
	// canonical identity, so consumers can match raw records back to it.
	Aliases map[string]bool `json:"-"`
}

// ChurnRatio reports removed lines as a fraction of added lines (0 when no
// additions). High churn means a lot of the author's added code was later
// rewritten or deleted.
func (a AuthorStats) ChurnRatio() float64 {
	if a.Added == 0 {
		return 0
	}
	return float64(a.Removed) / float64(a.Added)
}

// AIPercent reports the share of this author's commits that are AI-assisted.
func (a AuthorStats) AIPercent() int {
	if a.Commits == 0 {
		return 0
	}
	return a.AICommits * 100 / a.Commits
}

// RepoContribution holds per-repository stats for an author.
type RepoContribution struct {
	Commits     int `json:"commits"`
	Added       int `json:"added"`
	Removed     int `json:"removed"`
	Net         int `json:"net"`
	TotalChange int `json:"total_change"`
	AICommits   int `json:"ai_commits"`
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
		// Try fuzzy match against existing canonical names. Iterate candidates
		// in sorted order (not map order) so that when more than one canonical
		// is similar, the chosen match is deterministic across runs.
		cands := make([]string, 0, len(allCanonicals))
		for c := range allCanonicals {
			cands = append(cands, c)
		}
		sort.Strings(cands)
		for _, c := range cands {
			if NamesMatch(ae.author, c) {
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
	activeDays := make(map[string]map[string]bool) // canonical name → set of yyyy-mm-dd
	for _, r := range records {
		name := mergedCanonical[canonicalForAuthor[r.Author]]
		as, ok := byName[name]
		if !ok {
			as = &AuthorStats{
				Name:    name,
				PerRepo: make(map[string]*RepoContribution),
				Aliases: make(map[string]bool),
			}
			byName[name] = as
		}
		as.Commits++
		as.Added += r.Added
		as.Removed += r.Removed
		as.Net += r.Added - r.Removed
		as.TotalChange += r.Added + r.Removed
		as.Aliases[r.Author] = true
		if as.FirstCommit.IsZero() || r.Date.Before(as.FirstCommit) {
			as.FirstCommit = r.Date
		}
		if r.Date.After(as.LastCommit) {
			as.LastCommit = r.Date
		}
		if activeDays[name] == nil {
			activeDays[name] = make(map[string]bool)
		}
		activeDays[name][r.Date.Format("2006-01-02")] = true
		if r.AIAssisted {
			as.AICommits++
		}

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
		if r.AIAssisted {
			rc.AICommits++
		}
	}

	result := make([]AuthorStats, 0, len(byName))
	for _, as := range byName {
		as.ActiveDays = len(activeDays[as.Name])
		result = append(result, *as)
	}
	// Return in a stable order (by name) so callers that don't sort, and the
	// tie-handling in Sort, never depend on Go's randomized map iteration.
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// metricValue returns the value of the sort field for one author.
func metricValue(s AuthorStats, field SortField) int {
	switch field {
	case SortByCommits:
		return s.Commits
	case SortByAdded:
		return s.Added
	case SortByRemoved:
		return s.Removed
	case SortByNet:
		return s.Net
	case SortByAI:
		return s.AICommits
	default: // SortByTotal
		return s.TotalChange
	}
}

// Sort sorts stats descending by the given field. Ties are broken
// deterministically (TotalChange, then Commits, then Name) so the leaderboard
// — and which contributors survive the top-N cap — never reshuffle run-to-run.
func Sort(stats []AuthorStats, field SortField) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if va, vb := metricValue(a, field), metricValue(b, field); va != vb {
			return va > vb
		}
		if a.TotalChange != b.TotalChange {
			return a.TotalChange > b.TotalChange
		}
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		return a.Name < b.Name
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
	case "ai":
		return SortByAI
	default:
		return SortByTotal
	}
}

// NamesMatch reports whether two raw author names should be treated as the same
// identity under the current merge policy: exact normalized-name equality
// always, plus substring similarity when FuzzyMatching is enabled. This is the
// predicate the merge logic and the operative-view fallback both use, so the
// leaderboard and a contributor's timeline agree on who-is-who.
func NamesMatch(a, b string) bool {
	if normalizedName(a) == normalizedName(b) {
		return true
	}
	if FuzzyMatching {
		return AreSimilarNames(a, b)
	}
	return false
}

// NextSortField returns the next sort field in the cycle, wrapping around.
func NextSortField(f SortField) SortField {
	return (f + 1) % numSortFields
}

// SortFieldLabel returns a short human label for a sort field.
func SortFieldLabel(f SortField) string {
	switch f {
	case SortByCommits:
		return "COMMITS"
	case SortByAdded:
		return "ADDED"
	case SortByRemoved:
		return "REMOVED"
	case SortByNet:
		return "NET"
	case SortByAI:
		return "AI"
	default:
		return "IMPACT"
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
		if NamesMatch(name, candidate) {
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
