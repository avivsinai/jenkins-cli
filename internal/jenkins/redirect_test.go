package jenkins

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestIsLoginRedirect(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		target string
		want   bool
	}{
		{"commenceLogin same host", "http://jenkins.local:8080", "http://jenkins.local:8080/securityRealm/commenceLogin?from=%2Fapi%2Fjson", true},
		{"commenceLogin cross host", "http://jenkins.local:8080", "http://other.example.com/securityRealm/commenceLogin", true},
		{"commenceLogin under context path", "http://jenkins.local/jenkins", "http://jenkins.local/jenkins/securityRealm/commenceLogin", true},
		{"form login same host", "http://jenkins.local:8080", "http://jenkins.local:8080/login?from=%2F", true},
		{"form login trailing slash", "http://jenkins.local:8080", "http://jenkins.local:8080/login/", true},
		{"loginError same host", "http://jenkins.local:8080", "http://jenkins.local:8080/loginError", true},
		{"form login under context path", "http://jenkins.local/jenkins", "http://jenkins.local/jenkins/login", true},
		{"login prefix does not match", "http://jenkins.local:8080", "http://jenkins.local:8080/loginfoo", false},
		{"login outside context path does not match", "http://jenkins.local/jenkins", "http://jenkins.local/other/login", false},
		{"login on other host does not match", "http://jenkins.local:8080", "http://files.example.com/login", false},
		{"google accounts", "http://jenkins.local:8080", "https://accounts.google.com/o/oauth2/v2/auth?client_id=x", true},
		{"okta authorize", "http://jenkins.local:8080", "https://corp.okta.com/oauth2/v1/authorize?state=x", true},
		{"azure authorize", "http://jenkins.local:8080", "https://login.microsoftonline.com/tid/oauth2/v2.0/authorize", true},
		{"s3 presigned artifact", "http://jenkins.local:8080", "https://bucket.s3.amazonaws.com/artifacts/build.tgz?X-Amz-Signature=abc", false},
		{"same host api redirect", "http://jenkins.local:8080", "http://jenkins.local:8080/job/foo/api/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLoginRedirect(mustParse(t, tt.base), mustParse(t, tt.target))
			require.Equal(t, tt.want, got)
		})
	}
}

func newRedirectTestClient(baseURL string) *resty.Client {
	client := resty.New()
	client.SetBaseURL(baseURL)
	policy := ssoRedirectPolicy{base: mustParseNoT(baseURL)}
	client.SetRedirectPolicy(policy, resty.FlexibleRedirectPolicy(maxRedirects))
	return client
}

func mustParseNoT(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestSSORedirectPolicyBlocksLoginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/json":
			http.Redirect(w, r, "/securityRealm/commenceLogin?from=%2Fapi%2Fjson", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := newRedirectTestClient(server.URL)
	_, err := client.R().Get("/api/json")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSSORedirect)

	// net/http's url.Error wrapper echoes the raw redirect target;
	// sanitizeRedirectError (applied on every Client execution path) must
	// strip it down to the self-contained policy error.
	sanitized := sanitizeRedirectError(err)
	require.ErrorIs(t, sanitized, ErrSSORedirect)
	require.Contains(t, sanitized.Error(), "/me/configure")
	require.Contains(t, sanitized.Error(), "/whoAmI/api/json")
	require.NotContains(t, sanitized.Error(), "from=", "query parameters must not be echoed")
}

func TestSanitizeRedirectErrorPassthrough(t *testing.T) {
	require.NoError(t, sanitizeRedirectError(nil))
	plain := errors.New("boom")
	require.Equal(t, plain, sanitizeRedirectError(plain))
}

func TestSSORedirectPolicyFollowsNonLoginRedirect(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("artifact-bytes"))
	}))
	defer storage.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/artifacts/build.tgz", http.StatusFound)
	}))
	defer server.Close()

	client := newRedirectTestClient(server.URL)
	resp, err := client.R().Get("/job/foo/artifact/build.tgz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.Equal(t, "artifact-bytes", resp.String())
}

func TestSSORedirectPolicyPreservesRedirectCap(t *testing.T) {
	var server *httptest.Server
	hops := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", hops), http.StatusFound)
	}))
	defer server.Close()

	client := newRedirectTestClient(server.URL)
	_, err := client.R().Get("/hop/0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "stopped after 10 redirects")
}

func TestSSORedirectErrorSurvivesURLErrorWrapping(t *testing.T) {
	policy := ssoRedirectPolicy{base: mustParseNoT("http://jenkins.local:8080")}
	target, _ := http.NewRequest(http.MethodGet, "http://jenkins.local:8080/login", nil)
	err := policy.Apply(target, nil)
	require.ErrorIs(t, err, ErrSSORedirect)

	wrapped := &url.Error{Op: "Get", URL: "http://jenkins.local:8080/api/json", Err: err}
	require.True(t, errors.Is(wrapped, ErrSSORedirect))
}
