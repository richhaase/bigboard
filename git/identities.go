package git

import (
	"fmt"
	"net/mail"
	"slices"
	"strings"
)

type Identity struct {
	Aliases []string `json:"aliases"`
	Name    string   `json:"name,omitempty"`
}

func NormalizeIdentities(identities map[string]Identity) (map[string]Identity, error) {
	if len(identities) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(identities))
	for key := range identities {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make(map[string]Identity, len(identities))
	owners := make(map[string]string)
	for _, key := range keys {
		canonical, err := identityEmail(key)
		if err != nil {
			return nil, err
		}
		if _, exists := owners[canonical]; exists {
			return nil, fmt.Errorf("identity email %q appears more than once", canonical)
		}
		owners[canonical] = canonical
		result[canonical] = Identity{Name: strings.TrimSpace(identities[key].Name)}
	}
	for _, key := range keys {
		canonical := strings.ToLower(strings.TrimSpace(key))
		identity := result[canonical]
		for _, value := range identities[key].Aliases {
			alias, err := identityEmail(value)
			if err != nil {
				return nil, fmt.Errorf("identity %q: %w", canonical, err)
			}
			if owner, exists := owners[alias]; exists {
				return nil, fmt.Errorf("identity alias %q already belongs to %q", alias, owner)
			}
			owners[alias] = canonical
			identity.Aliases = append(identity.Aliases, alias)
		}
		result[canonical] = identity
	}
	return result, nil
}

func identityEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", fmt.Errorf("invalid identity email %q", value)
	}
	return email, nil
}

type canonicalIdentity struct {
	email string
	name  string
}

type identityResolver map[string]canonicalIdentity

func newIdentityResolver(identities map[string]Identity) (identityResolver, error) {
	normalized, err := NormalizeIdentities(identities)
	if err != nil {
		return nil, err
	}
	result := make(identityResolver)
	for email, identity := range normalized {
		canonical := canonicalIdentity{email: email, name: identity.Name}
		result[email] = canonical
		for _, alias := range identity.Aliases {
			result[alias] = canonical
		}
	}
	return result, nil
}

func (r identityResolver) resolve(name, email string) (string, string) {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if canonical, ok := r[strings.ToLower(email)]; ok {
		email = canonical.email
		if canonical.name != "" {
			name = canonical.name
		}
	}
	return name, email
}

func MergeIdentities(existing map[string]Identity, primary string, emails []string, name string) (map[string]Identity, error) {
	result, err := NormalizeIdentities(existing)
	if err != nil {
		return nil, err
	}
	primary, err = identityEmail(primary)
	if err != nil {
		return nil, err
	}
	owners := make(map[string]string)
	for canonical, identity := range result {
		owners[canonical] = canonical
		for _, alias := range identity.Aliases {
			owners[alias] = canonical
		}
	}
	selected := make(map[string]bool)
	groups := make(map[string]bool)
	for _, value := range emails {
		email, err := identityEmail(value)
		if err != nil {
			return nil, err
		}
		selected[email] = true
		owner := owners[email]
		if owner == "" {
			owner = email
		}
		groups[owner] = true
	}
	if !selected[primary] {
		return nil, fmt.Errorf("the primary identity must be selected")
	}
	if len(groups) < 2 {
		return nil, fmt.Errorf("select at least two different email identities")
	}
	identity := Identity{Name: strings.TrimSpace(name)}
	if identity.Name == "" {
		owner := owners[primary]
		if owner == "" {
			owner = primary
		}
		identity.Name = result[owner].Name
	}
	for owner := range groups {
		selected[owner] = true
		for _, alias := range result[owner].Aliases {
			selected[alias] = true
		}
		delete(result, owner)
	}
	delete(selected, primary)
	for email := range selected {
		identity.Aliases = append(identity.Aliases, email)
	}
	slices.Sort(identity.Aliases)
	if result == nil {
		result = make(map[string]Identity)
	}
	result[primary] = identity
	return NormalizeIdentities(result)
}
