package jenkins

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrSSORedirect marks requests that Jenkins (or a proxy in front of it)
// answered with a redirect to an interactive sign-in page instead of serving
// the API call. For a CLI request that always means the credentials never
// authenticated the request.
var ErrSSORedirect = errors.New("redirected to a sign-in page")

const maxRedirects = 10

// ssoRedirectPolicy aborts redirect chains that lead to an interactive login
// flow: the Jenkins form login, a security realm's SSO entry point
// (securityRealm/commenceLogin), or an external identity provider's authorize
// endpoint. All other redirects (artifact managers redirecting downloads to
// object storage, for example) are followed normally.
type ssoRedirectPolicy struct {
	base *url.URL
}

func (p ssoRedirectPolicy) Apply(req *http.Request, _ []*http.Request) error {
	if req.URL == nil || !isLoginRedirect(p.base, req.URL) {
		return nil
	}

	// IdP redirects carry sensitive query parameters (state, client_id);
	// report scheme+host+path only.
	sanitized := url.URL{Scheme: req.URL.Scheme, Host: req.URL.Host, Path: req.URL.Path}
	baseDisplay := strings.TrimSuffix(p.base.String(), "/")
	return fmt.Errorf("%w (%s): the request was not authenticated. "+
		"Controllers behind SSO (Google, OIDC, ...) still require a Jenkins API token for CLI access: "+
		"sign in with your browser, create a token at %s/me/configure, then run "+
		"'jk auth login %s --username <jenkins-user-id> --token <api-token>' "+
		"(if you do not know the Jenkins user ID, open %s/whoAmI/api/json in the signed-in browser and use its name value)",
		ErrSSORedirect, sanitized.String(), baseDisplay, baseDisplay, baseDisplay)
}

// sanitizeRedirectError unwraps the url.Error that net/http wraps around a
// CheckRedirect failure: that wrapper echoes the full redirect target
// including query parameters (IdP state, client_id), which must not reach
// user output. The inner policy error is self-contained and sanitized.
func sanitizeRedirectError(err error) error {
	if err == nil || !errors.Is(err, ErrSSORedirect) {
		return err
	}
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Err != nil && errors.Is(uerr.Err, ErrSSORedirect) {
		return uerr.Err
	}
	return err
}

// isLoginRedirect reports whether target is an interactive sign-in URL when
// reached from a Jenkins controller at base.
func isLoginRedirect(base, target *url.URL) bool {
	path := strings.ToLower(target.Path)

	// SecurityRealm SSO entry point on any host: the configured Jenkins root
	// URL may differ from the URL the client uses, so same-host matching is
	// not enough.
	if strings.Contains(path, "/securityrealm/commencelogin") {
		return true
	}

	// External identity providers.
	if strings.EqualFold(target.Hostname(), "accounts.google.com") {
		return true
	}
	if strings.Contains(path, "/oauth2/") && strings.Contains(path, "authorize") {
		return true
	}

	// Jenkins form login on the controller itself, anchored to its context
	// path so /loginfoo or unrelated /login endpoints elsewhere do not match.
	if strings.EqualFold(target.Host, base.Host) {
		rel := strings.TrimPrefix(path, strings.ToLower(strings.TrimSuffix(base.Path, "/")))
		switch strings.TrimSuffix(rel, "/") {
		case "/login", "/loginerror":
			return true
		}
	}

	return false
}
