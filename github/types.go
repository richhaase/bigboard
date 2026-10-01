// Package github provides bounded, read-only pull request context through the
// installed GitHub CLI. Authentication remains entirely the CLI's responsibility.
package github

import (
	"context"
	"errors"
	"strconv"
	"time"
)

const (
	// Unknown is deliberately different from an approval, successful check, or
	// merge-ready state. Null or unfamiliar API aggregate states become Unknown.
	Unknown = "UNKNOWN"

	// These limits bound one repository refresh. Hitting a limit retains useful
	// data and reports incompleteness instead of reporting an empty repository.
	MaxPullRequests = 200
	MaxFilesPerPR   = 1000
	MaxRequests     = 64
	MaxOutputBytes  = 2 * 1024 * 1024
	FetchTimeout    = 90 * time.Second
	RequestTimeout  = 15 * time.Second
)

var (
	ErrInvalidRepository = errors.New("invalid GitHub owner/repository")
	ErrUnsupportedOrigin = errors.New("origin is not a supported github.com repository URL")
	ErrUnavailable       = errors.New("GitHub CLI unavailable")
	ErrAuthentication    = errors.New("GitHub CLI authentication unavailable")
	ErrRateLimited       = errors.New("GitHub API rate limited")
	ErrRepository        = errors.New("GitHub repository unavailable")
	ErrRequest           = errors.New("GitHub request failed")
	ErrResponse          = errors.New("invalid GitHub API response")
	ErrIncomplete        = errors.New("GitHub context is incomplete")
	ErrBudgetExceeded    = errors.New("GitHub refresh limit reached")
	ErrOutputLimit       = errors.New("GitHub command output limit reached")
)

// Runner executes an argument-vector command, never a shell command. Tests can
// implement Runner without invoking git, gh, authentication, or the network.
type Runner interface {
	Run(ctx context.Context, command string, args ...string) ([]byte, error)
}

// File contains literal repository-relative path evidence. GraphQL does not
// expose rename/copy origins; PreviousPath is reserved for available evidence,
// and PreviousPathsComplete explicitly reports their absence.
type File struct {
	Path         string
	PreviousPath string
	ChangeType   string
}

// Review is a latest-per-reviewer review, not the aggregate review decision.
type Review struct {
	Author string
	State  string
}

// PullRequest is lightweight metadata only. It contains no body, comments,
// patches, check logs, credentials, or fetched Git objects. Display strings are
// untrusted remote text; callers must escape them for terminal rendering.
type PullRequest struct {
	Repo               string // canonical, lowercase owner/name
	Number             int
	URL                string
	Title              string
	Author             string
	RequestedReviewers []string // users are logins; teams are organization/slug
	Reviews            []Review
	ReviewDecision     string
	CheckState         string
	Mergeable          string
	UpdatedAt          time.Time
	Draft              bool
	Files              []File

	// FilesComplete concerns the list of current changed paths only.
	FilesComplete bool
	// PreviousPathsComplete is false when rename/copy origins are unavailable.
	PreviousPathsComplete      bool
	RequestedReviewersComplete bool
	ReviewsComplete            bool
	// ContextComplete covers all bounded supporting context, not the accuracy
	// of the independent aggregate decision/check/mergeability fields.
	ContextComplete bool
}

// Key deduplicates a PR across local clones that point at the same repository.
func (p PullRequest) Key() string {
	repo, err := CanonicalRepo(p.Repo)
	if err != nil || p.Number < 1 {
		return ""
	}
	return repo + "#" + strconv.Itoa(p.Number)
}

// Result distinguishes an empty, successful inventory from an unavailable or
// truncated one. Complete describes only the open-PR inventory; inspect each
// PR's completeness flags for bounded supporting context. Err can accompany
// useful partial data, including a complete inventory with incomplete files.
type Result struct {
	Repo     string
	PRs      []PullRequest
	Complete bool
	// RetryAt is shared quota cooldown metadata, including exhausted successful responses.
	RetryAt time.Time
	Err     error
}
