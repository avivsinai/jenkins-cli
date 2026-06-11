package auth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func TestAuthLoginUsernameHelpMentionsSSOUserID(t *testing.T) {
	cmd := newAuthLoginCmd(&cmdutil.Factory{})

	flag := cmd.Flags().Lookup("username")
	require.NotNil(t, flag)
	require.Contains(t, flag.Usage, "Jenkins user ID")
	require.Contains(t, flag.Usage, "Google/SSO users")
}

func TestAuthLoginInteractivePromptsDisambiguateJenkinsCredentials(t *testing.T) {
	require.Contains(t, usernamePrompt, "Jenkins user ID")
	require.Contains(t, usernamePrompt, "email")
	require.Equal(t, "Jenkins API token", tokenPrompt)
}

// loginTestEnv sandboxes config (HOME / XDG_CONFIG_HOME) and the secret store
// (encrypted file backend) into a temp directory.
func loginTestEnv(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")
}

func newLoginCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	return cmd, &out, &errOut
}

func openTestStore(t *testing.T) *secret.Store {
	t.Helper()
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	return store
}

func TestAuthLoginVerificationSuccess(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/whoAmI/api/json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"admin","authenticated":true}`))
	}))
	defer server.Close()

	cmd, out, _ := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true}

	require.NoError(t, runAuthLogin(cmd, cfg, opts, server.URL))
	require.Contains(t, out.String(), "as admin")

	saved, err := cfg.Context("t1")
	require.NoError(t, err)
	require.Equal(t, server.URL, saved.URL)

	got, err := openTestStore(t).Get(secret.TokenKey("t1"))
	require.NoError(t, err)
	require.Equal(t, "tok", got)
}

func TestAuthLoginVerificationRejectedRollsBackNewContext(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cmd, _, _ := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "bad", setActive: true}

	err := runAuthLogin(cmd, cfg, opts, server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rejected")
	require.Contains(t, err.Error(), "/me/configure")

	_, ctxErr := cfg.Context("t1")
	require.ErrorIs(t, ctxErr, config.ErrContextNotFound)
	require.Empty(t, cfg.Active)

	_, tokenErr := openTestStore(t).Get(secret.TokenKey("t1"))
	require.ErrorIs(t, tokenErr, os.ErrNotExist)
}

func TestAuthLoginVerificationRejectedRestoresPreviousState(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := &config.Config{}
	cfg.SetContext("t1", &config.Context{URL: "http://old.example.com", Username: "old-user"})
	require.NoError(t, cfg.SetActive("t1"))
	require.NoError(t, openTestStore(t).Set(secret.TokenKey("t1"), "old-token"))

	cmd, _, _ := newLoginCmd(t)
	opts := &authLoginOptions{name: "t1", username: "admin", token: "bad", setActive: true}

	err := runAuthLogin(cmd, cfg, opts, server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "restored")

	saved, ctxErr := cfg.Context("t1")
	require.NoError(t, ctxErr)
	require.Equal(t, "http://old.example.com", saved.URL)
	require.Equal(t, "old-user", saved.Username)
	require.Equal(t, "t1", cfg.Active)

	got, tokenErr := openTestStore(t).Get(secret.TokenKey("t1"))
	require.NoError(t, tokenErr)
	require.Equal(t, "old-token", got)
}

func TestAuthLoginVerificationSSORedirectRollsBack(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/whoAmI/api/json" {
			http.Redirect(w, r, "/securityRealm/commenceLogin?from=%2F", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cmd, _, _ := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true}

	err := runAuthLogin(cmd, cfg, opts, server.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, jenkins.ErrSSORedirect)
	require.Contains(t, err.Error(), "/me/configure")
	require.NotContains(t, err.Error(), "from=")

	_, ctxErr := cfg.Context("t1")
	require.ErrorIs(t, ctxErr, config.ErrContextNotFound)
}

func TestAuthLoginVerificationAnonymousRollsBack(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"anonymous","authenticated":false}`))
	}))
	defer server.Close()

	cmd, _, _ := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true}

	err := runAuthLogin(cmd, cfg, opts, server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "anonymous")

	_, ctxErr := cfg.Context("t1")
	require.ErrorIs(t, ctxErr, config.ErrContextNotFound)
}

func TestAuthLoginVerificationInconclusiveKeepsCredentials(t *testing.T) {
	loginTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd, out, errOut := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true}

	require.NoError(t, runAuthLogin(cmd, cfg, opts, server.URL))
	require.Contains(t, errOut.String(), "could not verify credentials")
	require.Contains(t, out.String(), "Logged in to")

	_, ctxErr := cfg.Context("t1")
	require.NoError(t, ctxErr)
}

func TestAuthLoginNoVerifySkipsCheck(t *testing.T) {
	loginTestEnv(t)

	cmd, out, _ := newLoginCmd(t)
	cfg := &config.Config{}
	// Unreachable URL: --no-verify must not touch the network.
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true, noVerify: true}

	require.NoError(t, runAuthLogin(cmd, cfg, opts, "http://127.0.0.1:1"))
	require.Contains(t, out.String(), "Logged in to")

	_, ctxErr := cfg.Context("t1")
	require.NoError(t, ctxErr)
}

func TestAuthLoginVerificationUnreachableIsInconclusive(t *testing.T) {
	loginTestEnv(t)

	cmd, _, errOut := newLoginCmd(t)
	cfg := &config.Config{}
	opts := &authLoginOptions{name: "t1", username: "admin", token: "tok", setActive: true}

	require.NoError(t, runAuthLogin(cmd, cfg, opts, "http://127.0.0.1:1"))
	require.Contains(t, errOut.String(), "could not verify credentials")

	_, ctxErr := cfg.Context("t1")
	require.NoError(t, ctxErr)
}

func TestLoginVerificationErrorWithoutPriorContext(t *testing.T) {
	parsed := mustURL(t, "http://jenkins.example.com")
	err := loginVerificationError(errors.New("boom"), parsed, "ctx1", false)
	require.Contains(t, err.Error(), `context "ctx1" was not saved`)
	require.Contains(t, err.Error(), "http://jenkins.example.com/me/configure")
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
