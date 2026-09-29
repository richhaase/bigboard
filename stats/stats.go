package stats

import (
	"cmp"
	"fmt"
	"math/bits"
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

// FuzzyMatching is retained for legacy name-comparison helpers. Aggregation
// always uses email/mailmap identities, irrespective of this setting.
var FuzzyMatching = false

// AggregateOptions controls contributor classification. FuzzyMatching is
// accepted for compatibility but no longer merges contributor identities.
type AggregateOptions struct {
	FuzzyMatching bool
	BotIdentities []string
}

// AuthorStats holds aggregated contribution data for a single author.
type AuthorStats struct {
	ID                 string                       `json:"-"`
	UnknownLineCommits int                          `json:"unknown_line_commits,omitempty"`
	Name               string                       `json:"name"`
	Commits            int                          `json:"commits"`
	Added              int                          `json:"added"`
	Removed            int                          `json:"removed"`
	Net                int                          `json:"net"`
	TotalChange        int                          `json:"total_change"`
	AICommits          int                          `json:"ai_commits"`
	Bot                bool                         `json:"bot"`
	FirstCommit        time.Time                    `json:"first_commit"`
	LastCommit         time.Time                    `json:"last_commit"`
	ActiveDays         int                          `json:"active_days"`
	PerRepo            map[string]*RepoContribution `json:"per_repo,omitempty"`
	// Aliases contains observed name spellings. Use ID to match records;
	// a name can belong to more than one person.
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
	UnknownLineCommits int `json:"unknown_line_commits,omitempty"`
	Commits            int `json:"commits"`
	Added              int `json:"added"`
	Removed            int `json:"removed"`
	Net                int `json:"net"`
	TotalChange        int `json:"total_change"`
	AICommits          int `json:"ai_commits"`
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

// IdentityID uses mailmapped email, never name similarity. Missing-email
// identities remain local to their repository and exact author name.
func IdentityID(r git.CommitRecord) string {
	if email := strings.ToLower(strings.TrimSpace(r.Email)); email != "" {
		return "email:" + email
	}
	repo := r.RepoID
	if repo == "" {
		repo = r.RepoName
	}
	return fmt.Sprintf("missing:%q:%q", repo, r.Author)
}

func groupCommits(records []git.CommitRecord) [][]git.CommitRecord {
	var groups [][]git.CommitRecord
	byID := make(map[string]int)
	for _, r := range records {
		if index, ok := byID[r.CommitID]; r.CommitID != "" && ok {
			groups[index] = append(groups[index], r)
		} else {
			if r.CommitID != "" {
				byID[r.CommitID] = len(groups)
			}
			groups = append(groups, []git.CommitRecord{r})
		}
	}
	return groups
}

// Prefer a complete copy, then choose attribution deterministically if clones
// have conflicting mailmaps. Repository associations do not duplicate totals.
func preferredCopy(copies []git.CommitRecord) git.CommitRecord {
	best := copies[0]
	for _, r := range copies[1:] {
		if (best.LinesUnknown && !r.LinesUnknown) || (best.LinesUnknown == r.LinesUnknown &&
			(r.RepoID < best.RepoID || (r.RepoID == best.RepoID && IdentityID(r)+"\x00"+r.Author < IdentityID(best)+"\x00"+best.Author))) {
			best = r
		}
	}
	return best
}

// UniqueRecords supplies the same commit selection to detail charts and totals.
// Records without object IDs are distinct (including legacy API callers).
func UniqueRecords(records []git.CommitRecord) []git.CommitRecord {
	groups := groupCommits(records)
	result := make([]git.CommitRecord, 0, len(groups))
	for _, copies := range groups {
		result = append(result, preferredCopy(copies))
	}
	return result
}

// AggregateWithOptions groups unique commits by email/mailmap identity and
// returns totals in deterministic name order, retaining repository associations.
func AggregateWithOptions(records []git.CommitRecord, options AggregateOptions) []AuthorStats {
	byID := make(map[string]*AuthorStats)
	names := make(map[string]map[string]int)
	days := make(map[string]map[string]bool)
	for _, copies := range groupCommits(records) {
		r := preferredCopy(copies)
		id := IdentityID(r)
		as := byID[id]
		if as == nil {
			as = &AuthorStats{ID: id, PerRepo: make(map[string]*RepoContribution), Aliases: make(map[string]bool)}
			byID[id] = as
			names[id] = make(map[string]int)
			days[id] = make(map[string]bool)
		}
		names[id][r.Author]++
		for _, copy := range copies {
			if IdentityID(copy) == id {
				as.Aliases[copy.Author] = true
			}
		}
		as.Bot = as.Bot || IsBotIdentity(r.Author, r.Email, options.BotIdentities)
		as.Commits++
		added, removed := r.Added, r.Removed
		if r.LinesUnknown {
			added, removed = 0, 0
			as.UnknownLineCommits++
		}
		as.Added += added
		as.Removed += removed
		as.Net += added - removed
		as.TotalChange += added + removed
		date := r.Date.In(time.Local)
		if as.FirstCommit.IsZero() || date.Before(as.FirstCommit) {
			as.FirstCommit = date
		}
		if date.After(as.LastCommit) {
			as.LastCommit = date
		}
		days[id][date.Format("2006-01-02")] = true
		if r.AIAssisted {
			as.AICommits++
		}
		repos := make(map[string]bool)
		for _, copy := range copies {
			repos[copy.RepoName] = true
		}
		for name := range repos {
			rc := as.PerRepo[name]
			if rc == nil {
				rc = &RepoContribution{}
				as.PerRepo[name] = rc
			}
			rc.Commits++
			rc.Added += added
			rc.Removed += removed
			rc.Net += added - removed
			rc.TotalChange += added + removed
			if r.LinesUnknown {
				rc.UnknownLineCommits++
			}
			if r.AIAssisted {
				rc.AICommits++
			}
		}
	}
	result := make([]AuthorStats, 0, len(byID))
	for id, as := range byID {
		bestCount := -1
		for name, count := range names[id] {
			if count > bestCount || (count == bestCount && preferCanonical(name, as.Name)) {
				as.Name, bestCount = name, count
			}
		}
		as.ActiveDays = len(days[id])
		result = append(result, *as)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// Compare nonnegative fractions with a 128-bit product so precision is not
// lost to display rounding or multiplication overflow.
func compareAIRatio(a, b AuthorStats) int {
	an, ad := unsignedCount(a.AICommits), unsignedCount(a.Commits)
	bn, bd := unsignedCount(b.AICommits), unsignedCount(b.Commits)
	if ad == 0 {
		an, ad = 0, 1
	}
	if bd == 0 {
		bn, bd = 0, 1
	}
	ah, al := bits.Mul64(an, bd)
	bh, bl := bits.Mul64(bn, ad)
	if c := cmp.Compare(ah, bh); c != 0 {
		return c
	}
	return cmp.Compare(al, bl)
}

func unsignedCount(value int) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
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
		if field == SortByAI {
			if c := compareAIRatio(a, b); c != 0 {
				return c > 0
			}
		} else if va, vb := metricValue(a, field), metricValue(b, field); va != vb {
			return va > vb
		}
		if a.TotalChange != b.TotalChange {
			return a.TotalChange > b.TotalChange
		}
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
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

// NamesMatch is a legacy name-similarity helper, not an identity test.
func NamesMatch(a, b string) bool {
	return namesMatch(a, b, FuzzyMatching)
}

// NamesMatchWithOptions compares name similarity for legacy callers.
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
