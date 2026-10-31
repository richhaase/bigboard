package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type installation struct {
	ID      int64
	Account *struct {
		Login string
		Type  string
	}
}

type discoverySession struct {
	provider *Provider
	requests int
	limit    int
}

func (s *discoverySession) request(ctx context.Context, endpoint string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.requests >= s.limit {
		return nil, ErrBudgetExceeded
	}
	s.requests++
	return s.provider.conditionalRequest(ctx, endpoint)
}

func (s *discoverySession) installations(ctx context.Context) ([]installation, error) {
	var result []installation
	seen := make(map[int64]bool)
	total := -1
	for page := 1; ; page++ {
		data, err := s.request(ctx, fmt.Sprintf("user/installations?per_page=100&page=%d", page))
		if err != nil {
			return nil, err
		}
		var response struct {
			TotalCount    *int `json:"total_count"`
			Installations []*installation
		}
		if json.Unmarshal(data, &response) != nil || response.Installations == nil {
			return nil, ErrResponse
		}
		done, err := completeDiscoveryPage(response.TotalCount, &total, len(result), len(response.Installations))
		if err != nil {
			return nil, err
		}
		for _, row := range response.Installations {
			if row == nil || row.ID <= 0 || row.Account == nil || (row.Account.Type != "Organization" && row.Account.Type != "User") {
				return nil, ErrResponse
			}
			login := row.Account.Login
			if row.Account.Type == "User" {
				login = strings.ReplaceAll(login, "_", "a")
			}
			if !validOwner(login) {
				return nil, ErrResponse
			}
			if seen[row.ID] {
				return nil, ErrIncomplete
			}
			seen[row.ID] = true
			result = append(result, *row)
		}
		if done {
			return result, nil
		}
	}
}

func completeDiscoveryPage(count *int, total *int, loaded, rows int) (bool, error) {
	if count == nil || *count < 0 || rows > 100 || loaded+rows > *count {
		return false, ErrResponse
	}
	if *total >= 0 && *total != *count {
		return false, ErrIncomplete
	}
	*total = *count
	if loaded+rows == *count {
		return true, nil
	}
	if rows < 100 {
		return false, ErrIncomplete
	}
	return false, nil
}

func (p *Provider) DiscoverAccessibleRepositories(ctx context.Context) ([]DiscoveredRepository, error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	session := discoverySession{provider: p, limit: MaxRefreshRequests}
	return session.repositories(ctx, nil)
}

func (s *discoverySession) repositories(ctx context.Context, sources map[string]string) ([]DiscoveredRepository, error) {
	installations, err := s.installations(ctx)
	if err != nil {
		return nil, err
	}
	if sources != nil {
		selected := make([]installation, 0, len(sources))
		accessible := make(map[string]bool)
		for _, installed := range installations {
			owner := strings.ToLower(installed.Account.Login)
			if sources[owner] == installed.Account.Type {
				selected = append(selected, installed)
				accessible[owner] = true
			}
		}
		if len(accessible) != len(sources) {
			return nil, ErrPermission
		}
		installations = selected
	}
	result := []DiscoveredRepository{}
	seen := make(map[string]bool)
	for _, installed := range installations {
		loaded, total := 0, -1
		installationSeen := make(map[string]bool)
		for page := 1; ; page++ {
			endpoint := fmt.Sprintf("user/installations/%d/repositories?per_page=100&page=%d", installed.ID, page)
			data, err := s.request(ctx, endpoint)
			if err != nil {
				if errors.Is(err, ErrPermission) || errors.Is(err, ErrRepository) {
					return nil, &InstallationAccessError{Owner: strings.ToLower(installed.Account.Login), Err: err}
				}
				return nil, err
			}
			var response struct {
				TotalCount   *int `json:"total_count"`
				Repositories []*struct {
					Name          string `json:"full_name"`
					DefaultBranch string `json:"default_branch"`
					Archived      bool
					PushedAt      time.Time `json:"pushed_at"`
				}
			}
			if json.Unmarshal(data, &response) != nil || response.Repositories == nil {
				return nil, ErrResponse
			}
			done, err := completeDiscoveryPage(response.TotalCount, &total, loaded, len(response.Repositories))
			if err != nil {
				return nil, err
			}
			loaded += len(response.Repositories)
			for _, row := range response.Repositories {
				if row == nil {
					return nil, ErrResponse
				}
				name, err := CanonicalRepo(row.Name)
				owner, _, _ := strings.Cut(name, "/")
				if err != nil || !strings.EqualFold(owner, installed.Account.Login) {
					return nil, ErrResponse
				}
				if installationSeen[name] {
					return nil, ErrIncomplete
				}
				installationSeen[name] = true
				if !row.Archived && !seen[name] {
					seen[name] = true
					result = append(result, DiscoveredRepository{OwnerType: installed.Account.Type, Name: name, DefaultBranch: row.DefaultBranch, PushedAt: row.PushedAt})
				}
			}
			if done {
				break
			}
		}
	}
	slices.SortFunc(result, func(a, b DiscoveredRepository) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}
