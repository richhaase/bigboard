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
	numSortFields
)

// FuzzyMatching enables substring-based name merging across different emails.
// Off by default because it over-merges distinct people (Daniel/Daniela);
// enable via the --fuzzy flag.
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

// ChurnRatio reports removed lines as a fraction of added lines (0 when there
// are no additions).
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

// Aggregate groups records by contributor identity and computes per-author
// totals, returned in a deterministic name-sorted order.
func Aggregate(records []git.CommitRecord) []AuthorStats {
	emailToCanonical := canonicalNameByEmail(records)
	canonicalForAuthor := assignCanonicalNames(records, emailToCanonical)
	mergedCanonical := mergeSimilarCanonicals(records, canonicalForAuthor)
	return aggregateByCanonical(records, canonicalForAuthor, mergedCanonical)
}

func canonicalNameByEmail(records []git.CommitRecord) map[string]string {
	longest := make(map[string]string)
	for _, r := range records {
		email := strings.ToLower(r.Email)
		if email == "" {
			continue
		}
		if len(r.Author) > len(longest[email]) {
			longest[email] = r.Author
		}
	}
	return longest
}

func assignCanonicalNames(records []git.CommitRecord, emailToCanonical map[string]string) map[string]string {
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

	canonicalForAuthor := make(map[string]string)
	for _, ae := range pairs {
		if ae.email != "" {
			canonicalForAuthor[ae.author] = emailToCanonical[ae.email]
		}
	}

	known := make(map[string]bool)
	for _, c := range canonicalForAuthor {
		known[c] = true
	}
	for _, ae := range pairs {
		if _, ok := canonicalForAuthor[ae.author]; ok {
			continue
		}
		candidates := make([]string, 0, len(known))
		for c := range known {
			candidates = append(candidates, c)
		}
		sort.Strings(candidates)
		for _, c := range candidates {
			if NamesMatch(ae.author, c) {
				canonicalForAuthor[ae.author] = c
				break
			}
		}
		if _, ok := canonicalForAuthor[ae.author]; !ok {
			canonicalForAuthor[ae.author] = ae.author
			known[ae.author] = true
		}
	}
	return canonicalForAuthor
}

func mergeSimilarCanonicals(records []git.CommitRecord, canonicalForAuthor map[string]string) map[string]string {
	unique := make(map[string]bool)
	for _, c := range canonicalForAuthor {
		unique[c] = true
	}
	canonicalList := make([]string, 0, len(unique))
	for c := range unique {
		canonicalList = append(canonicalList, c)
	}
	sort.Strings(canonicalList)

	commitCounts := make(map[string]int)
	for _, r := range records {
		commitCounts[canonicalForAuthor[r.Author]]++
	}
	return buildCanonicalMap(canonicalList, commitCounts)
}

func aggregateByCanonical(records []git.CommitRecord, canonicalForAuthor, mergedCanonical map[string]string) []AuthorStats {
	byName := make(map[string]*AuthorStats)
	activeDays := make(map[string]map[string]bool)
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
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

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
		return s.AIPercent()
	default:
		return s.TotalChange
	}
}

// Sort sorts stats descending by the given field. Ties are broken
// deterministically (TotalChange, then Commits, then Name) so the leaderboard —
// and which contributors survive the top-N cap — never reshuffle run-to-run.
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

// NamesMatch reports whether two raw author names are the same identity under
// the current merge policy: normalized-name equality always, plus substring
// similarity when FuzzyMatching is enabled.
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

// AreSimilarNames reports whether two names match case-insensitively with
// whitespace normalization, or one is a substring of the other (for names
// longer than 5 characters).
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
// choosing the highest-commit-count name among similar names. Ties break to the
// longer name, then lexicographically, so the result is independent of allNames
// ordering.
func MergeAuthorName(name string, allNames []string, commitCounts map[string]int) string {
	best := name
	bestCount := commitCounts[name]

	for _, candidate := range allNames {
		if candidate == name || !NamesMatch(name, candidate) {
			continue
		}
		count := commitCounts[candidate]
		if count > bestCount || (count == bestCount && preferCanonical(candidate, best)) {
			bestCount = count
			best = candidate
		}
	}
	return best
}

func preferCanonical(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a < b
}

func buildCanonicalMap(allNames []string, commitCounts map[string]int) map[string]string {
	canonical := make(map[string]string, len(allNames))
	for _, name := range allNames {
		canonical[name] = MergeAuthorName(name, allNames, commitCounts)
	}
	return canonical
}

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
