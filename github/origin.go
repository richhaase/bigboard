package github

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
)

// CanonicalRepo validates one exact owner/name identifier. It accepts neither
// URLs nor extra path segments, escaping, credentials, or command-line syntax.
func CanonicalRepo(value string) (string, error) {
	owner, name, found := strings.Cut(value, "/")
	if !found || !validOwner(owner) || !validName(name) {
		return "", ErrInvalidRepository
	}
	return strings.ToLower(owner + "/" + name), nil
}

func validOwner(s string) bool {
	if len(s) == 0 || len(s) > 39 || !asciiAlnum(s[0]) || !asciiAlnum(s[len(s)-1]) {
		return false
	}
	for i := range len(s) {
		if !asciiAlnum(s[i]) && s[i] != '-' {
			return false
		}
	}
	return true
}

func validName(s string) bool {
	if len(s) == 0 || len(s) > 100 || s == "." || s == ".." {
		return false
	}
	for i := range len(s) {
		if !asciiAlnum(s[i]) && s[i] != '-' && s[i] != '_' && s[i] != '.' {
			return false
		}
	}
	return true
}

func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// ParseOrigin accepts only https://github.com/owner/name,
// ssh://git@github.com/owner/name, or git@github.com:owner/name (with an
// optional .git suffix and one trailing slash). Host matching is case-insensitive.
// Credentials, ports, queries, fragments, escaping, alternate hosts, git://,
// local paths, and ambiguous path syntax are deliberately unsupported.
func ParseOrigin(origin string) (string, error) {
	if origin == "" || strings.ContainsAny(origin, "\\%?#") {
		return "", ErrUnsupportedOrigin
	}
	for _, c := range origin {
		if c <= ' ' || c >= 127 {
			return "", ErrUnsupportedOrigin
		}
	}
	var path string
	if strings.HasPrefix(origin, "git@") && !strings.Contains(origin, "://") {
		host, tail, ok := strings.Cut(strings.TrimPrefix(origin, "git@"), ":")
		if !ok || !strings.EqualFold(host, "github.com") || strings.HasPrefix(tail, "/") {
			return "", ErrUnsupportedOrigin
		}
		path = tail
	} else {
		u, err := url.Parse(origin)
		if err != nil || u.Opaque != "" || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
			return "", ErrUnsupportedOrigin
		}
		switch u.Scheme {
		case "https":
			if u.User != nil {
				return "", ErrUnsupportedOrigin
			}
		case "ssh":
			if u.User == nil || u.User.Username() != "git" {
				return "", ErrUnsupportedOrigin
			}
			if _, hasPassword := u.User.Password(); hasPassword {
				return "", ErrUnsupportedOrigin
			}
		default:
			return "", ErrUnsupportedOrigin
		}
		if !strings.HasPrefix(u.Path, "/") {
			return "", ErrUnsupportedOrigin
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	repo, err := CanonicalRepo(path)
	if err != nil {
		return "", ErrUnsupportedOrigin
	}
	return repo, nil
}

// Resolve reads only the local origin URL, without following includes, applying
// insteadOf rewrites, contacting a remote, or reading authentication settings.
func (p *Provider) Resolve(ctx context.Context, localPath string) (string, error) {
	path, err := filepath.Abs(localPath)
	if err != nil || localPath == "" || strings.ContainsRune(localPath, '\x00') {
		return "", ErrUnsupportedOrigin
	}
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	out, err := p.runner.Run(ctx, "git", "--no-optional-locks", "-C", path,
		"-c", "core.fsmonitor=false", "config", "--local", "--no-includes", "--get-all", "remote.origin.url")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrUnsupportedOrigin
	}
	if len(out) > MaxOutputBytes {
		return "", ErrOutputLimit
	}
	// Only the command's single final newline is formatting, not URL content.
	origin := strings.TrimSuffix(string(out), "\n")
	return ParseOrigin(origin)
}
