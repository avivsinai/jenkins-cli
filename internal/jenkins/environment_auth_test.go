package jenkins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

func TestResolveAuthContextEnvironment(t *testing.T) {
	tests := []struct {
		name, selected, endpoint, user, token, active string
		wantName, wantSource, wantError               string
	}{
		{name: "stored", active: "prod", wantName: "prod", wantSource: "stored"},
		{name: "token override", active: "prod", token: "env-secret", wantName: "prod", wantSource: "environment"},
		{name: "ephemeral", endpoint: "https://ci.example/jenkins/", user: "alice", token: "env-secret", wantName: "environment", wantSource: "environment"},
		{name: "matching tuple", active: "prod", endpoint: "https://ci.example/jenkins/", user: "alice", token: "env-secret", wantName: "prod", wantSource: "environment"},
		{name: "URL conflict", active: "prod", endpoint: "https://other.example", user: "alice", token: "env-secret", wantError: "conflict with selected context"},
		{name: "user conflict", active: "prod", endpoint: "https://ci.example/jenkins", user: "bob", token: "env-secret", wantError: "conflict with selected context"},
		{name: "explicit missing context", selected: "missing", endpoint: "https://ci.example", user: "alice", token: "env-secret", wantError: "context not found"},
		{name: "token alone without context", token: "env-secret", wantError: "requires a selected context"},
		{name: "missing URL", active: "prod", user: "alice", token: "env-secret", wantError: "complete JK_URL, JK_USER and JK_TOKEN tuple"},
		{name: "missing user", endpoint: "https://ci.example", token: "env-secret", wantError: "complete JK_URL, JK_USER and JK_TOKEN tuple"},
		{name: "missing token", endpoint: "https://ci.example", user: "alice", wantError: "complete JK_URL, JK_USER and JK_TOKEN tuple"},
		{name: "invalid scheme", endpoint: "file:///tmp/controller", user: "alice", token: "env-secret", wantError: "http or https"},
		{name: "userinfo", endpoint: "https://alice:url-secret@ci.example", user: "alice", token: "env-secret", wantError: "without credentials"},
		{name: "query", endpoint: "https://ci.example?token=url-secret", user: "alice", token: "env-secret", wantError: "without credentials"},
		{name: "empty variables", endpoint: " ", user: " ", token: " ", active: "prod", wantName: "prod", wantSource: "stored"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JK_URL", tt.endpoint)
			t.Setenv("JK_USER", tt.user)
			t.Setenv("JK_TOKEN", tt.token)
			cfg := &config.Config{}
			cfg.SetContext("prod", &config.Context{URL: "https://ci.example/jenkins", Username: "alice"})
			require.NoError(t, cfg.SetActive(tt.active))
			before, err := json.Marshal(cfg)
			require.NoError(t, err)
			definition, name, source, err := ResolveAuthContext(cfg, tt.selected)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.NotContains(t, err.Error(), "env-secret")
				require.NotContains(t, err.Error(), "url-secret")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantName, name)
				require.Equal(t, tt.wantSource, source)
				require.Equal(t, "https://ci.example/jenkins", definition.URL)
				require.Equal(t, "alice", definition.Username)
			}
			after, err := json.Marshal(cfg)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "resolution must not mutate configuration")
		})
	}
}

func TestEnvironmentTokenBypassesUnusableKeyringWithoutWrites(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		t.Run(map[bool]string{false: "stored context", true: "no config"}[ephemeral], func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", tmp)
			t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
			t.Setenv("KEYRING_BACKEND", "file")
			t.Setenv("KEYRING_FILE_DIR", filepath.Join(tmp, "not-a-directory"))
			require.NoError(t, os.WriteFile(filepath.Join(tmp, "not-a-directory"), []byte("blocked"), 0o600))
			t.Setenv("JK_TOKEN", "ephemeral-secret")
			t.Setenv("JK_URL", "")
			t.Setenv("JK_USER", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, token, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "alice", user)
				require.Equal(t, "ephemeral-secret", token)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			cfg, err := config.Load()
			require.NoError(t, err)
			if ephemeral {
				t.Setenv("JK_URL", server.URL)
				t.Setenv("JK_USER", "alice")
			} else {
				cfg.SetContext("prod", &config.Context{URL: server.URL, Username: "alice"})
				require.NoError(t, cfg.SetActive("prod"))
			}
			client, err := NewClient(context.Background(), cfg, "", WithSkipCapabilityProbe(), WithDisableWarn(true))
			require.NoError(t, err)
			_, err = client.Do(client.NewRequest(), http.MethodGet, "/check", nil)
			require.NoError(t, err)
			_, err = client.Do(client.NewStreamingRequest(), http.MethodGet, "/check", nil)
			require.NoError(t, err)
			_, err = os.Stat(cfg.Path())
			require.ErrorIs(t, err, os.ErrNotExist)
			entries, err := os.ReadDir(tmp)
			require.NoError(t, err)
			require.Len(t, entries, 1, "auth must not create config or secret files")
		})
	}
}
