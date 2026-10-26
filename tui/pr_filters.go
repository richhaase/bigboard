package tui

import (
	"sort"
	"strings"

	gh "github.com/richhaase/bigboard/github"
)

type prFilters struct {
	author, review, checks, state filterValues
}

type prFilterOption struct {
	value, label string
}

func (f prFilters) matches(pr gh.PullRequest) bool {
	author := strings.ToLower(pr.Author)
	if author == "" {
		author = "?"
	}
	state := "open"
	if pr.Draft {
		state = "draft"
	}
	return f.author.includes(author) &&
		f.review.includes(prReviewLabel(pr.ReviewDecision)) &&
		f.checks.includes(prCheckLabel(pr.CheckState)) &&
		f.state.includes(state)
}

func (f prFilters) empty() bool {
	return len(f.author)+len(f.review)+len(f.checks)+len(f.state) == 0
}

func (f prFilters) clone() prFilters {
	return prFilters{author: f.author.clone(), review: f.review.clone(), checks: f.checks.clone(), state: f.state.clone()}
}

func (f prFilters) values(kind string) filterValues {
	switch kind {
	case "author":
		return f.author
	case "review":
		return f.review
	case "checks":
		return f.checks
	case "state":
		return f.state
	default:
		return nil
	}
}

func (f *prFilters) setValues(kind string, values filterValues) {
	switch kind {
	case "author":
		f.author = values.clone()
	case "review":
		f.review = values.clone()
	case "checks":
		f.checks = values.clone()
	case "state":
		f.state = values.clone()
	}
}

func (m Model) prFilterOptions() []prFilterOption {
	options := []prFilterOption{{"", "All"}}
	switch m.prFilterKind {
	case "author":
		options[0].label = "All authors"
		authors := map[string]string{}
		for _, pr := range m.sharedPRs() {
			value := strings.ToLower(pr.Author)
			if value == "" {
				value = "?"
			}
			authors[value] = githubIdentity(value)
			if value == "?" {
				authors[value] = "Unknown author"
			}
		}
		for _, active := range m.prFilters.author {
			if authors[active] == "" {
				authors[active] = githubIdentity(active)
				if active == "?" {
					authors[active] = "Unknown author"
				}
			}
		}
		for value, label := range authors {
			options = append(options, prFilterOption{value, label})
		}
		sort.Slice(options[1:], func(i, j int) bool { return options[i+1].value < options[j+1].value })
	case "review":
		options[0].label = "All review states"
		for _, label := range []string{"Needs review", "Approved", "Changes req.", "Unknown"} {
			options = append(options, prFilterOption{label, label})
		}
	case "checks":
		options[0].label = "All check states"
		for _, label := range []string{"Failing", "Error", "Pending", "Expected", "Passing", "Unknown"} {
			options = append(options, prFilterOption{label, label})
		}
	case "state":
		options[0].label = "All open PRs (including drafts)"
		options = append(options, prFilterOption{"open", "● Open, excluding drafts"}, prFilterOption{"draft", "◇ Drafts only"})
	default:
		options[0].label = "All included repositories"
		for _, repo := range m.prScopeRepositories() {
			options = append(options, prFilterOption{repo.ID, displayText(repo.Name)})
		}
	}
	if m.prFilterQuery == "" {
		return options
	}
	filtered := options[:0]
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.label), strings.ToLower(m.prFilterQuery)) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}
