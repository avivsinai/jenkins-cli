package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

const watchTestSHA = "2222222222222222222222222222222222222222"

func watchTestClient(t *testing.T, handler http.HandlerFunc) (*jenkins.Client, *config.Config) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "watch-test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "watch-test-pass")
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("test"), "test-token"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{}
	cfg.SetContext("test", &config.Context{URL: server.URL, Username: "test", AllowInsecureStore: true})
	require.NoError(t, cfg.SetActive("test"))
	client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)
	return client, cfg
}

func watchTestBuild(n int64) runSummary {
	return runSummary{Number: n, URL: fmt.Sprintf("http://jenkins/job/app/%d/", n), Building: true,
		Actions: []map[string]any{{"lastBuiltRevision": map[string]any{"SHA1": watchTestSHA}}}}
}

func TestWatchCommitDiscoveryQueuePinAndCompletion(t *testing.T) {
	var discovery, detailCalls, queueCalls int
	client, _ := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/api/json":
			discovery++
			builds := []runSummary{}
			if discovery >= 3 {
				builds = []runSummary{watchTestBuild(7), watchTestBuild(8)}
			}
			require.NoError(t, json.NewEncoder(w).Encode(runListResponse{Builds: builds}))
		case "/job/app/8/api/json":
			detailCalls++
			detail := runDetail{Number: 8, URL: "http://jenkins/job/app/8/", Building: detailCalls < 3, Duration: 7500}
			if !detail.Building {
				detail.Result = "FAILURE"
			}
			require.NoError(t, json.NewEncoder(w).Encode(detail))
		case "/queue/api/json":
			queueCalls++
			items := []queueListItem{{ID: 100, Why: "unrelated secret job", Task: queueTaskRef{URL: "http://jenkins/job/other/"}}}
			if queueCalls <= 2 {
				items = append(items, queueListItem{ID: 3, Why: fmt.Sprintf("Build #6 is already in progress (ETA: %d min)", queueCalls), Task: queueTaskRef{URL: "http://jenkins/job/app/"}})
			}
			if queueCalls == 3 {
				items = append(items,
					queueListItem{ID: 5, Why: "no executor", Task: queueTaskRef{URL: "job/app/8/"}},
					queueListItem{ID: 6, Why: "wrong build", Task: queueTaskRef{URL: "job/app/9/"}})
			}
			require.NoError(t, json.NewEncoder(w).Encode(queueListResponse{Items: items}))
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	var progress bytes.Buffer
	output := runWatchOutput{JobPath: "app", Commit: watchTestSHA}
	err := watchCommitRun(context.Background(), client, &runWatchOptions{commit: watchTestSHA, interval: time.Millisecond}, &output, &progress)
	require.NoError(t, err)
	require.Equal(t, 3, discovery, "stop discovery after choosing the newest matching build")
	require.Equal(t, int64(8), output.Number)
	require.Equal(t, "FAILURE", output.Result)
	require.Equal(t, "completed", output.Status)
	require.Equal(t, int64(7500), output.DurationMs)
	require.Equal(t, 1, strings.Count(progress.String(), "Job queued"), "changing ETA should not generate progress noise")
	require.Contains(t, progress.String(), "commit not yet identified")
	require.Contains(t, progress.String(), "Waiting for an executor for #8: queue #5: no executor")
	require.Contains(t, progress.String(), "Running #8")
	require.Contains(t, progress.String(), "Finished #8: FAILURE")
	require.NotContains(t, progress.String(), "unrelated secret job")
	require.NotContains(t, progress.String(), "wrong build")
}

func TestFindCommitRunPagesPastDefaultLimit(t *testing.T) {
	var calls int
	client, _ := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		builds := []runSummary{}
		if calls == 1 {
			require.Contains(t, r.URL.Query().Get("tree"), "{0,100}")
			for n := int64(200); n > 100; n-- {
				builds = append(builds, runSummary{Number: n})
			}
		} else {
			require.Contains(t, r.URL.Query().Get("tree"), "{100,200}")
			builds = append(builds, watchTestBuild(100))
		}
		require.Contains(t, r.URL.Query().Get("tree"), "revision[hash,pullHash]")
		require.NoError(t, json.NewEncoder(w).Encode(runListResponse{Builds: builds}))
	})
	got, err := findCommitRun(context.Background(), client, "app", watchTestSHA)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(100), got.Number)
	require.Equal(t, 2, calls)
}

func TestWatchCancellationInterruptsHTTPRequest(t *testing.T) {
	cancelled := make(chan struct{})
	client, _ := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(cancelled)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	output := runWatchOutput{JobPath: "app", Commit: watchTestSHA}
	started := time.Now()
	err := watchCommitRun(ctx, client, &runWatchOptions{commit: watchTestSHA, interval: time.Hour}, &output, &bytes.Buffer{})
	require.Error(t, err)
	require.Less(t, time.Since(started), time.Second)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request was not cancelled")
	}
}

func TestWatchTaskBuildCorrelation(t *testing.T) {
	for _, task := range []string{"job/app/8/", "http://jenkins/jenkins/job/app/8/", "/job/app/8"} {
		require.True(t, watchTaskIsBuild(task, "app", 8), task)
	}
	for _, task := range []string{"job/app/", "job/app/9/", "job/other/8/", "job/app/18/", "job/app/8/foo", "job/folder/job/app/8/", "%zz"} {
		require.False(t, watchTaskIsBuild(task, "app", 8), task)
	}
	require.True(t, watchTaskIsBuild("job/team/job/my%20job/8/", "team/my job", 8))
	require.True(t, watchTaskIsJob("job/app/", "app"))
	require.True(t, watchTaskIsJob("http://jenkins/jenkins/job/app/", "app"))
	require.False(t, watchTaskIsJob("job/folder/job/app/", "app"))
	require.True(t, watchTaskIsJob("job/app/job/feature%252Flogin/", "app/feature%2Flogin"))
}

func newWatchTestCommand(client *jenkins.Client, cfg *config.Config) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return cfg, nil },
		JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) { return client, nil }}
	root := &cobra.Command{Use: "jk", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().Bool("json", false, "JSON")
	root.PersistentFlags().Bool("yaml", false, "YAML")
	root.PersistentFlags().String("format", "", "Format")
	root.AddCommand(NewCmdRun(f))
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	return root, stdout, stderr
}

func TestWatchCommandJSONAndResultCodes(t *testing.T) {
	for _, tc := range []struct {
		result string
		code   int
	}{{"SUCCESS", 0}, {"UNSTABLE", 10}, {"FAILURE", 11}, {"ABORTED", 12}, {"NOT_BUILT", 13}, {"", 1}, {"NEW_RESULT", 1}} {
		t.Run(tc.result, func(t *testing.T) {
			client, cfg := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/job/app/api/json" {
					require.NoError(t, json.NewEncoder(w).Encode(runListResponse{Builds: []runSummary{watchTestBuild(8)}}))
				} else {
					require.NoError(t, json.NewEncoder(w).Encode(runDetail{Number: 8, Result: tc.result, Duration: 1500}))
				}
			})
			cmd, stdout, stderr := newWatchTestCommand(client, cfg)
			cmd.SetArgs([]string{"run", "watch", "app", "--commit", watchTestSHA, "--json", "--exit-status"})
			err := cmd.Execute()
			if tc.code == 0 {
				require.NoError(t, err)
			} else {
				var exit *cmdutil.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, tc.code, exit.Code)
			}
			decoder := json.NewDecoder(stdout)
			var output runWatchOutput
			require.NoError(t, decoder.Decode(&output))
			require.Equal(t, int64(8), output.Number)
			require.Equal(t, "completed", output.Status)
			var extra any
			require.Error(t, decoder.Decode(&extra), "only a single JSON object belongs on stdout")
			require.Contains(t, stderr.String(), "Finished #8")
		})
	}
}

func TestWatchCommandTimeoutAndCancelOutput(t *testing.T) {
	for _, timedOut := range []bool{true, false} {
		t.Run(fmt.Sprint(timedOut), func(t *testing.T) {
			var calls atomic.Int32
			client, cfg := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = fmt.Fprint(w, `{"builds":[],"items":[]}`)
			})
			cmd, stdout, _ := newWatchTestCommand(client, cfg)
			args := []string{"run", "watch", "app", "--commit", watchTestSHA, "--json", "--interval", "1h"}
			if timedOut {
				args = append(args, "--timeout", "10ms")
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				cmd.SetContext(ctx)
			}
			cmd.SetArgs(args)
			var exit *cmdutil.ExitError
			require.ErrorAs(t, cmd.Execute(), &exit)
			var got runWatchOutput
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
			if timedOut {
				require.Equal(t, 124, exit.Code)
				require.Equal(t, "timed_out", got.Status)
				require.Positive(t, calls.Load())
			} else {
				require.Equal(t, 130, exit.Code)
				require.Equal(t, "cancelled", got.Status)
				require.Zero(t, calls.Load())
			}
			require.Zero(t, got.Number)
		})
	}
}

func TestWatchCommandValidation(t *testing.T) {
	for _, flags := range [][]string{{}, {"--commit", "abc"}, {"--commit", watchTestSHA, "--interval", "0s"}, {"--commit", watchTestSHA, "--interval", "-1s"}, {"--commit", watchTestSHA, "--timeout", "-1s"}} {
		cmd, _, _ := newWatchTestCommand(nil, nil)
		cmd.SetArgs(append([]string{"run", "watch", "app"}, flags...))
		require.Error(t, cmd.Execute())
	}
}

func TestWatchHTTPErrorIsNotCompletion(t *testing.T) {
	client, _ := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	output := runWatchOutput{JobPath: "app", Commit: watchTestSHA}
	err := watchCommitRun(context.Background(), client, &runWatchOptions{commit: watchTestSHA, interval: time.Millisecond}, &output, &bytes.Buffer{})
	require.Error(t, err)
	require.NotEqual(t, "completed", output.Status)
	require.False(t, errors.Is(err, context.Canceled))
}
