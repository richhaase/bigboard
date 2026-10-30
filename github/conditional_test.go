package github

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func taggedDiscovery(step discoveryStep, etag string) discoveryStep {
	step.response.Header = http.Header{"Etag": {etag}}
	return step
}
func notModifiedDiscovery() discoveryStep {
	return discoveryStep{response: Response{StatusCode: http.StatusNotModified}}
}

func TestAccessibleDiscoveryReusesConditionalInstallationAndRepositoryPages(t *testing.T) {
	first := repositoryRow("example/api")
	retired := repositoryRow("example/old")
	retired["archived"] = true
	client := &discoveryClient{t: t, steps: []discoveryStep{
		taggedDiscovery(installationPage(t, 1, installationRow(1, "example", "Organization")), `"installation"`),
		taggedDiscovery(repositoryPage(t, 2, first, retired), `W/"repositories"`),
		notModifiedDiscovery(), notModifiedDiscovery(),
		notModifiedDiscovery(), taggedDiscovery(repositoryPage(t, 2, first, repositoryRow("example/new")), `"changed"`),
	}}
	provider := NewProvider(client)
	for range 2 {
		got, err := provider.DiscoverAccessibleRepositories(t.Context())
		if err != nil || !reflect.DeepEqual(got, []DiscoveredRepository{{Name: "example/api", DefaultBranch: "main"}}) {
			t.Fatalf("conditional discovery=%+v err=%v", got, err)
		}
	}
	got, err := provider.DiscoverAccessibleRepositories(t.Context())
	if err != nil || len(got) != 2 || got[1].Name != "example/new" {
		t.Fatalf("changed repositories=%+v err=%v", got, err)
	}
	for i, call := range client.calls {
		want := ""
		if i >= 2 {
			want = `"installation"`
			if i%2 == 1 {
				want = `W/"repositories"`
			}
		}
		if call.Header.Get("If-None-Match") != want {
			t.Fatalf("request %d missing matching validator", i)
		}
	}
}

func TestDiscoverPersonalRepositoriesReusesIdentityAndInstallationValidators(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{
		taggedDiscovery(discoveryJSON(t, map[string]any{"login": "example"}), `"user"`),
		taggedDiscovery(installationPage(t, 1, installationRow(1, "example", "User")), `"installations"`),
		taggedDiscovery(repositoryPage(t, 1, repositoryRow("example/private")), `"repositories"`),
		notModifiedDiscovery(), notModifiedDiscovery(), notModifiedDiscovery(),
	}}
	provider := NewProvider(client)
	for range 2 {
		got, err := provider.DiscoverPersonalRepositories(t.Context(), "example")
		if err != nil || len(got) != 1 {
			t.Fatalf("personal conditional repositories=%+v err=%v", got, err)
		}
	}
	for i, tag := range []string{`"user"`, `"installations"`, `"repositories"`} {
		if client.calls[i+3].Header.Get("If-None-Match") != tag {
			t.Fatalf("conditional validator missing for %s", client.calls[i+3].Endpoint)
		}
	}
}

func TestConditionalRequestRejectsUnusableResponses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first bool
		step  discoveryStep
		want  error
	}{
		{"304 without cache", false, notModifiedDiscovery(), ErrResponse},
		{"not found", false, discoveryStep{response: Response{StatusCode: 404}}, ErrRepository},
		{"redirect", false, discoveryStep{response: Response{StatusCode: 302}}, ErrRequest},
		{"authentication with 304", true, discoveryStep{response: Response{StatusCode: 304}, err: ErrAuthentication}, ErrAuthentication},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := []discoveryStep{}
			if tc.first {
				steps = append(steps, taggedDiscovery(installationPage(t, 0), `"cached"`))
			}
			steps = append(steps, tc.step)
			client := &discoveryClient{t: t, steps: steps}
			provider := NewProvider(client)
			if tc.first {
				if _, err := provider.conditionalRequest(t.Context(), "user/installations"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := provider.conditionalRequest(t.Context(), "user/installations"); !errors.Is(err, tc.want) {
				t.Fatalf("conditional error=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestConditionalRequestForgetsValidatorAfterFailure(t *testing.T) {
	client := &discoveryClient{t: t, steps: []discoveryStep{taggedDiscovery(installationPage(t, 0), `"cached"`), {err: ErrRateLimited}, installationPage(t, 0)}}
	provider := NewProvider(client)
	if _, err := provider.conditionalRequest(t.Context(), "user/installations"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.conditionalRequest(t.Context(), "user/installations"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	if _, err := provider.conditionalRequest(t.Context(), "user/installations"); !errors.Is(err, ErrRateLimited) || len(client.calls) != 2 {
		t.Fatalf("cooldown ignored: calls=%d error=%v", len(client.calls), err)
	}
	retry := provider.cooldown().Add(time.Second)
	provider.now = func() time.Time { return retry }
	if _, err := provider.conditionalRequest(t.Context(), "user/installations"); err != nil || client.calls[2].Header.Get("If-None-Match") != "" {
		t.Fatalf("failed validator reused: %v", err)
	}
}

func TestRejectsUnsafeETags(t *testing.T) {
	for _, etag := range []string{"", "a b", "\"a\"\n", string(make([]byte, maxETagLength+1))} {
		if validETag(etag) {
			t.Fatalf("unsafe validator accepted")
		}
	}
}
