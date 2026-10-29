package shared

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
)

// Wire fixtures follow PipelineGraph/PipelineStepList and PipelineJsonWriterTest
// in jenkinsci/pipeline-graph-view-plugin (9626b6c); fields such as state and
// stageId belong to each node, and the endpoints wrap their payload in data.
const pipelineTreeFixture = `{"status":"ok","data":{"complete":true,"stages":[{"id":"2","name":"Tests","state":"failure","type":"STAGE","children":[{"id":"5","name":"Linux","state":"success","type":"PARALLEL","children":[]}],"nextSibling":{"id":"8","name":"Publish","state":"skipped","type":"STAGE","children":[]}}]}}`
const pipelineStepsFixture = `{"status":"ok","data":{"runIsComplete":true,"steps":[{"id":"3","name":"unit checks","state":"failure","type":"STEP","title":"Shell Script","stageId":"2"},{"id":"6","name":"unit checks","state":"success","type":"STEP","title":"Shell Script","stageId":"5"}]}}`

func newPipelineClient(t *testing.T, url string) *jenkins.Client {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp)
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("test"), "read-only-token"))
	cfg := &config.Config{}
	cfg.SetContext("test", &config.Context{URL: url, Username: "reader", AllowInsecureStore: true})
	client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)
	return client
}

func TestFetchPipelineSteps(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		user, token, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "reader", user)
		require.Equal(t, "read-only-token", token)
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/job/team/job/app/17/api/json":
			_, _ = w.Write([]byte(`{"number":17}`))
		case "/job/team/job/app/17/stages/tree":
			_, _ = w.Write([]byte(pipelineTreeFixture))
		case "/job/team/job/app/17/stages/allSteps":
			_, _ = w.Write([]byte(pipelineStepsFixture))
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	output, err := FetchPipelineSteps(context.Background(), newPipelineClient(t, srv.URL), "team/app", 17)
	require.NoError(t, err)
	require.Len(t, requests, 3)
	require.True(t, output.Complete)
	require.Equal(t, "failure", output.Steps[0].State)
	require.Equal(t, "2", output.Steps[0].StageID)
	require.Equal(t, output.Steps[0].Name, output.Steps[1].Name)
	for _, id := range []string{"2", "3", "5", "6", "8"} {
		require.True(t, output.HasNode(id), id)
	}
	require.False(t, output.HasNode("999"))
}

func TestFetchPipelineStepsErrorsAndEmptyRuns(t *testing.T) {
	for _, tc := range []struct {
		name, body, want      string
		buildCode, pluginCode int
	}{
		{name: "absent run", buildCode: 404, want: "run team/app #17 not found"},
		{name: "unauthorized run", buildCode: 401, want: "401 Unauthorized"},
		{name: "forbidden run", buildCode: 403, want: "403 Forbidden"},
		{name: "absent plugin", pluginCode: 404, want: "Pipeline Graph View is unavailable"},
		{name: "forbidden plugin", pluginCode: 403, want: "403 Forbidden"},
		{name: "invalid JSON", body: "<html>login</html>", want: "decode Pipeline Graph View"},
		{name: "plugin error", body: `{"status":"error","data":{"message":"failed"}}`, want: `status "error"`},
		{name: "missing data", body: `{"status":"ok"}`, want: "decode Pipeline Graph View data"},
		{name: "missing stages", body: `{"status":"ok","data":{}}`, want: `missing "stages"`},
		{name: "empty running", body: `{"status":"ok","data":{"stages":[],"complete":false,"steps":[],"runIsComplete":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pluginRequests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/api/json") {
					if tc.buildCode != 0 {
						w.WriteHeader(tc.buildCode)
					}
					_, _ = w.Write([]byte(`{"number":17}`))
					return
				}
				pluginRequests++
				if tc.pluginCode != 0 {
					w.WriteHeader(tc.pluginCode)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			result, err := FetchPipelineSteps(context.Background(), newPipelineClient(t, srv.URL), "team/app", 17)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				if tc.buildCode != 0 {
					require.Zero(t, pluginRequests)
				}
				return
			}
			require.NoError(t, err)
			require.False(t, result.Complete)
			require.NotNil(t, result.Stages)
			require.NotNil(t, result.Steps)
		})
	}
}
