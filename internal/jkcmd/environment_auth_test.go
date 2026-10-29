package jkcmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	jkfactory "github.com/avivsinai/jenkins-cli/pkg/cmd/factory"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/root"
	"github.com/avivsinai/jenkins-cli/pkg/iostreams"
	"github.com/stretchr/testify/require"
)

func TestAdversarialEnvironmentAuthThroughRootFactory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("JK_CONTEXT", "")
	t.Setenv("KEYRING_BACKEND", "unusable-backend")
	t.Setenv("JK_TOKEN", "review-secret")
	t.Setenv("JK_USER", "review-user")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, token, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "review-user", user)
		require.Equal(t, "review-secret", token)
		if r.URL.Path == "/queue/api/json" {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	t.Setenv("JK_URL", srv.URL)
	f, err := jkfactory.New("review")
	require.NoError(t, err)
	ios, _, out, errOut := iostreams.Test()
	f.IOStreams = ios
	cmd, err := root.NewCmdRoot(f)
	require.NoError(t, err)
	cmd.SetArgs([]string{"queue", "ls", "--json"})
	code := execute(cmd, errOut)
	require.Equal(t, 0, code, errOut.String())
	require.Equal(t, 1, calls)
	require.NotContains(t, out.String()+errOut.String(), "review-secret")
	_, err = os.Stat(filepath.Join(tmp, "jk"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
