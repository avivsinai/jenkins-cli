package run

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
)

func TestFetchRunDetailUsesSCMRevisionWithPipelineLibraryCheckout(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")

	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("test"), "token"))

	// The default API depth omits SCMRevisionAction's revision, and the
	// library checkout's BuildData comes first.
	const detailJSON = `{"number":7,"result":"SUCCESS","actions":[
		{"_class":"jenkins.scm.api.SCMRevisionAction"},
		{"_class":"hudson.plugins.git.util.BuildData","lastBuiltRevision":{"SHA1":"1111111111111111111111111111111111111111","branch":[{"name":"library-branch"}]},"remoteUrls":["https://example.com/org/pipeline-library.git"]},
		{"_class":"hudson.plugins.git.util.BuildData","lastBuiltRevision":{"SHA1":"2222222222222222222222222222222222222222","branch":[{"name":"main"}]},"remoteUrls":["https://example.com/org/app.git"]}
	]}`
	var trees []string
	var failLookup atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.Equal(t, "/job/app/job/main/7/api/json", r.URL.Path)
		tree := r.URL.Query().Get("tree")
		trees = append(trees, tree)
		if tree == "actions[_class,revision[hash,pullHash]]" {
			if failLookup.Load() {
				http.Error(w, "lookup denied", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"actions":[{"_class":"jenkins.scm.api.SCMRevisionAction","revision":{"hash":"2222222222222222222222222222222222222222"}},{},{}]}`))
			return
		}
		_, _ = w.Write([]byte(detailJSON))
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.SetContext("test", &config.Context{URL: srv.URL, Username: "alice", AllowInsecureStore: true})
	client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)

	detail, err := fetchRunDetail(client, "app/main", 7)
	require.NoError(t, err)
	require.Equal(t, []string{"", "actions[_class,revision[hash,pullHash]]"}, trees)

	info := extractSCMInfo("app/main", detail.Actions, detail.ChangeSet)
	require.NotNil(t, info)
	require.Equal(t, "2222222222222222222222222222222222222222", info.Commit)
	require.Equal(t, "main", info.Branch)
	require.Equal(t, "https://example.com/org/app.git", info.Repo)

	failLookup.Store(true)
	_, err = fetchRunDetail(client, "app/main", 7)
	require.ErrorContains(t, err, "source revision")
}
