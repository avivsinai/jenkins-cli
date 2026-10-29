package logcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func stepLogFactory(t *testing.T, url string) *cmdutil.Factory {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp)
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("test"), "token"))
	cfg := &config.Config{Active: "test"}
	cfg.SetContext("test", &config.Context{URL: url, Username: "reader", AllowInsecureStore: true})
	client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)
	return &cmdutil.Factory{Config: func() (*config.Config, error) { return cfg, nil }, JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) { return client, nil }}
}

func TestStepLogCommand(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		body      string
		running   bool
		wantError string
		truncated bool
	}{
		{name: "plain Unicode", args: []string{"--plain", "--step", "3"}, body: "failure ☃\n"},
		{name: "YAML", args: []string{"--yaml", "--step", "3"}, body: "failure\n"},
		{name: "JSON", args: []string{"--json", "--step", "3"}, body: "failure\n"},
		{name: "running JSON", args: []string{"--json", "--step", "3"}, body: "still working\n", running: true},
		{name: "invalid node", args: []string{"--step", "999"}, wantError: "node \"999\" not found"},
		{name: "bounded JSON", args: []string{"--json", "--step", "3"}, body: strings.Repeat("x", 2*1024*1024+100), truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logRequests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/api/json"):
					_ = json.NewEncoder(w).Encode(map[string]any{"number": 17, "building": tc.running, "result": "FAILURE"})
				case strings.HasSuffix(r.URL.Path, "/stages/tree"):
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": map[string]any{"complete": !tc.running, "stages": []any{}}})
				case strings.HasSuffix(r.URL.Path, "/stages/allSteps"):
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": map[string]any{"runIsComplete": !tc.running, "steps": []any{map[string]any{"id": "3", "name": "unit checks", "state": "failure", "type": "STEP", "stageId": "2"}}}})
				case strings.HasSuffix(r.URL.Path, "/stages/log"):
					logRequests++
					require.Equal(t, "3", r.URL.Query().Get("nodeId"))
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			}))
			defer srv.Close()
			cmd := NewCmdLog(stepLogFactory(t, srv.URL))
			cmd.PersistentFlags().Bool("json", false, "")
			cmd.PersistentFlags().Bool("yaml", false, "")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"team/app", "17"}, tc.args...))
			err := cmd.Execute()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Zero(t, logRequests)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, logRequests)
			if tc.name == "plain Unicode" {
				require.Equal(t, tc.body, out.String())
				return
			}
			var output logOutput
			if tc.name == "YAML" {
				require.NoError(t, yaml.Unmarshal(out.Bytes(), &output))
			} else {
				require.NoError(t, json.Unmarshal(out.Bytes(), &output))
			}
			require.Equal(t, "3", output.StepID)
			require.Equal(t, !tc.running && !tc.truncated, output.Complete)
			require.Equal(t, tc.truncated, output.Truncated)
			require.LessOrEqual(t, len(output.Log), 2*1024*1024)
		})
	}
}

func TestStepLogRejectsUnsupportedFlagsBeforeAuth(t *testing.T) {
	for _, args := range [][]string{{"--step", ""}, {"--step", "3", "--follow"}, {"--step", "3", "--tail", "2"}, {"--step", "3", "--grep", "failed"}} {
		cmd := NewCmdLog(nil)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		if cmd.Flags().Lookup("tail") == nil {
			cmd.Flags().Int("tail", 0, "")
		}
		if cmd.Flags().Lookup("grep") == nil {
			cmd.Flags().String("grep", "", "")
		}
		cmd.SetArgs(append([]string{"team/app", "17"}, args...))
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "--step")
	}
}
