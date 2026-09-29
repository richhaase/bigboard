package stats

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

// FuzzyMatching is retained for compatibility with Aggregate, NamesMatch, and
// MergeAuthorName. New code should pass AggregateOptions to
// AggregateWithOptions instead.
var FuzzyMatching = false

// AggregateOptions controls contributor identity resolution.
type AggregateOptions struct {
	FuzzyMatching bool
	BotIdentities []string
}

// AuthorStats holds aggregated contribution data for a single author.
type AuthorStats struct {
	Name        string                       `json:"name"`
	Commits     int                          `json:"commits"`
	Added       int                          `json:"added"`
	Removed     int                          `json:"removed"`
	Net         int                          `json:"net"`
	TotalChange int                          `json:"total_change"`
	AICommits   int                          `json:"ai_commits"`
	Bot         bool                         `json:"bot"`
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

// FilterByRepo returns records not in the excluded set. Keys may be stable
// repository IDs or repository names; ID keys allow precise filtering when
// multiple repositories share a name.
func FilterByRepo(records []git.CommitRecord, excluded map[string]bool) []git.CommitRecord {
	if len(excluded) == 0 {
		return records
	}
	out := make([]git.CommitRecord, 0, len(records))
	for _, r := range records {
		if !excluded[r.RepoID] && !excluded[r.RepoName] {
			out = append(out, r)
		}
	}
	return out
}

// Aggregate groups records by contributor identity and computes per-author
// totals, returned in a deterministic name-sorted order.
func Aggregate(records []git.CommitRecord) []AuthorStats {
	return AggregateWithOptions(records, AggregateOptions{FuzzyMatching: FuzzyMatching})
}

// AggregateWithOptions groups records by contributor identity using explicit
// options and returns totals in deterministic name order.
func AggregateWithOptions(records []git.CommitRecord, options AggregateOptions) []AuthorStats {
	canonical := resolveCanonicalNames(records, options.FuzzyMatching)
	return aggregateByCanonical(records, canonical, options)
}

type identityKey struct {
	author string
	email  string
}

func keyForRecord(record git.CommitRecord) identityKey {
	return identityKey{
		author: record.Author,
		email:  strings.ToLower(strings.TrimSpace(record.Email)),
	}
}

type disjointSet []int

func newDisjointSet(n int) disjointSet {
	set := make(disjointSet, n)
	for i := range set {
		set[i] = i
	}
	return set
}

func (s disjointSet) find(i int) int {
	root := i
	for s[root] != root {
		root = s[root]
	}
	for s[i] != i {
		parent := s[i]
		s[i] = root
		i = parent
	}
	return root
}

func (s disjointSet) union(a, b int) {
	a, b = s.find(a), s.find(b)
	if a == b {
		return
	}
	if a < b {
		s[b] = a
	} else {
		s[a] = b
	}
}

func resolveCanonicalNames(records []git.CommitRecord, fuzzy bool) map[identityKey]string {
	pairIndex := make(map[identityKey]int)
	pairs := make([]identityKey, 0)
	for _, record := range records {
		key := keyForRecord(record)
		if _, ok := pairIndex[key]; ok {
			continue
		}
		pairIndex[key] = len(pairs)
		pairs = append(pairs, key)
	}

	set := newDisjointSet(len(pairs))
	byEmail := make(map[string]int)
	byName := make(map[string]int)
	for i, pair := range pairs {
		if pair.email != "" {
			if previous, ok := byEmail[pair.email]; ok {
				set.union(i, previous)
			} else {
				byEmail[pair.email] = i
			}
		}
		name := normalizedName(pair.author)
		if name == "" {
			continue
		}
		if previous, ok := byName[name]; ok {
			set.union(i, previous)
		} else {
			byName[name] = i
		}
	}

	if fuzzy {
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			for _, candidate := range names[i+1:] {
				if similarNormalizedNames(name, candidate) {
					set.union(byName[name], byName[candidate])
				}
			}
		}
	}

	nameCounts := make(map[int]map[string]int)
	for _, record := range records {
		root := set.find(pairIndex[keyForRecord(record)])
		if nameCounts[root] == nil {
			nameCounts[root] = make(map[string]int)
		}
		nameCounts[root][record.Author]++
	}

	canonicalByRoot := make(map[int]string, len(nameCounts))
	for root, counts := range nameCounts {
		best := ""
		bestCount := -1
		for name, count := range counts {
			if count > bestCount || (count == bestCount && preferCanonical(name, best)) {
				best = name
				bestCount = count
			}
		}
		canonicalByRoot[root] = best
	}

	canonical := make(map[identityKey]string, len(pairs))
	for i, pair := range pairs {
		canonical[pair] = canonicalByRoot[set.find(i)]
	}
	return canonical
}

func aggregateByCanonical(records []git.CommitRecord, canonical map[identityKey]string, options AggregateOptions) []AuthorStats {
	byName := make(map[string]*AuthorStats)
	activeDays := make(map[string]map[string]bool)
	for _, r := range records {
		name := canonical[keyForRecord(r)]
		as, ok := byName[name]
		if !ok {
			as = &AuthorStats{
				Name:    name,
				PerRepo: make(map[string]*RepoContribution),
				Aliases: make(map[string]bool),
			}
			byName[name] = as
		}
		if !as.Bot && IsBotIdentity(r.Author, r.Email, options.BotIdentities) {
			as.Bot = true
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

// ParseSortField converts a string to a SortField.
func ParseSortField(s string) (SortField, error) {
	switch strings.ToLower(s) {
	case "commits":
		return SortByCommits, nil
	case "added":
		return SortByAdded, nil
	case "removed":
		return SortByRemoved, nil
	case "net":
		return SortByNet, nil
	case "ai":
		return SortByAI, nil
	case "total", "impact":
		return SortByTotal, nil
	}
	return SortByTotal, fmt.Errorf("invalid sort %q (want commits|added|removed|net|ai|total)", s)
}

// SortFieldFromString converts a string to a SortField, defaulting to
// SortByTotal for compatibility with older callers.
func SortFieldFromString(s string) SortField {
	field, _ := ParseSortField(s)
	return field
}

// NamesMatch reports whether two raw author names are the same identity under
// the current merge policy: normalized-name equality always, plus substring
// similarity when FuzzyMatching is enabled.
func NamesMatch(a, b string) bool {
	return namesMatch(a, b, FuzzyMatching)
}

// NamesMatchWithOptions reports whether two raw author names are the same
// identity under the supplied merge policy.
func NamesMatchWithOptions(a, b string, options AggregateOptions) bool {
	return namesMatch(a, b, options.FuzzyMatching)
}

func namesMatch(a, b string, fuzzy bool) bool {
	normalizedA := normalizedName(a)
	normalizedB := normalizedName(b)
	if normalizedA == normalizedB {
		return true
	}
	if fuzzy {
		return similarNormalizedNames(normalizedA, normalizedB)
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
	return similarNormalizedNames(normalizedName(a), normalizedName(b))
}

func similarNormalizedNames(a, b string) bool {
	if a == b {
		return true
	}
	if utf8.RuneCountInString(a) > 5 && strings.Contains(b, a) {
		return true
	}
	if utf8.RuneCountInString(b) > 5 && strings.Contains(a, b) {
		return true
	}
	return false
}

// MergeAuthorName returns the canonical name for the given name from allNames,
// choosing the highest-commit-count name among similar names. Ties break to the
// longer name, then lexicographically, so the result is independent of allNames
// ordering.
func MergeAuthorName(name string, allNames []string, commitCounts map[string]int) string {
	return mergeAuthorName(name, allNames, commitCounts, FuzzyMatching)
}

func mergeAuthorName(name string, allNames []string, commitCounts map[string]int, fuzzy bool) string {
	best := name
	bestCount := commitCounts[name]

	for _, candidate := range allNames {
		if candidate == name || !namesMatch(name, candidate, fuzzy) {
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
	aLength := utf8.RuneCountInString(a)
	bLength := utf8.RuneCountInString(b)
	if aLength != bLength {
		return aLength > bLength
	}
	return a < b
}

var builtinBotNames = map[string]bool{
	"dependabot":         true,
	"dependabot-preview": true,
	"renovate":           true,
	"github-actions":     true,
	"snyk-bot":           true,
	"greenkeeper":        true,
	"imgbot":             true,
	"mergify":            true,
	"allcontributors":    true,
	"pre-commit-ci":      true,
	"codecov":            true,
}

// IsBotIdentity reports whether an author name/email pair is a bot account.
// Extra entries match an exact email, an "@domain" suffix, or an exact name.
func IsBotIdentity(name, email string, extra []string) bool {
	rawName := strings.ToLower(strings.TrimSpace(name))
	address := strings.ToLower(strings.Trim(strings.TrimSpace(email), "<> "))
	for _, entry := range extra {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		switch {
		case strings.HasPrefix(entry, "@"):
			if strings.HasSuffix(address, entry) {
				return true
			}
		case strings.Contains(entry, "@"):
			if address == entry {
				return true
			}
		default:
			if rawName == entry {
				return true
			}
		}
	}
	if strings.Contains(rawName, "[bot]") {
		return true
	}
	local, _, _ := strings.Cut(address, "@")
	if strings.Contains(local, "[bot]") {
		return true
	}
	base := strings.TrimSpace(strings.TrimSuffix(rawName, "[bot]"))
	base = strings.TrimSpace(strings.TrimSuffix(base, " bot"))
	return builtinBotNames[base]
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
