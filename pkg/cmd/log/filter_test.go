package logcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func TestSelectLogLines(t *testing.T) {
	for _, tt := range []struct {
		name, input, pattern, want string
		tail, budget               int
		truncated                  bool
	}{
		{name: "empty", tail: 10},
		{name: "empty lines", input: "\n\n\n", tail: 2, want: "\n\n"},
		{name: "no final newline", input: "a\nb\nc", tail: 2, want: "b\nc"},
		{name: "Unicode CRLF", input: "first\r\n错误: 甲\r\nother\r\n错误: 乙\r\n", pattern: "^错误:", tail: 1, want: "错误: 乙\r\n"},
		{name: "filter before tail", input: "match 1\nno\nmatch 2\nno\n", pattern: "^match", tail: 2, want: "match 1\nmatch 2\n"},
		{name: "no matches", input: "nothing\nhere", pattern: "missing", tail: 10},
		{name: "anchors exclude CRLF", input: "a\r\nb\r\n", pattern: "^a$", want: "a\r\n"},
		{name: "grep output budget", input: "aaa\nbbb\nccc\n", pattern: ".", budget: 8, want: "aaa\nbbb\n", truncated: true},
		{name: "tail output budget", input: "aaa\nbbb\nccc\n", tail: 3, budget: 8, want: "bbb\nccc\n", truncated: true},
		{name: "early budget eviction no longer needed", input: "1234567\na\nb\n", tail: 2, budget: 8, want: "a\nb\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := &logOptions{tailLines: tt.tail, maxBytes: tt.budget}
			if tt.pattern != "" {
				opts.grep = regexp.MustCompile(tt.pattern)
			}
			got, truncated, err := selectLogLines(context.Background(), strings.NewReader(tt.input), opts)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
			require.Equal(t, tt.truncated, truncated)
		})
	}
}

func TestFilteredLogScansBeyondSnapshotBudget(t *testing.T) {
	// Much larger than the snapshot budget, including a match at the real end.
	input := strings.Repeat("prefix content\n", 300000) + "failure at the real end\n"
	got, truncated, err := selectLogLines(context.Background(), strings.NewReader(input), &logOptions{tailLines: 1, grep: regexp.MustCompile("failure")})
	require.NoError(t, err)
	require.False(t, truncated)
	require.Equal(t, "failure at the real end\n", string(got))
}

func TestFilteredLogRejectsOversizedLines(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("x", maxFilteredLineBytes+1),
		strings.Repeat("x", maxFilteredLineBytes+1) + "\nend\n",
	} {
		_, _, err := selectLogLines(context.Background(), strings.NewReader(input), &logOptions{tailLines: 1})
		require.ErrorContains(t, err, "maximum line")
	}
	// The documented maximum includes its line ending.
	_, truncated, err := selectLogLines(context.Background(), strings.NewReader(strings.Repeat("x", maxFilteredLineBytes-1)+"\n"), &logOptions{tailLines: 1})
	require.NoError(t, err)
	require.False(t, truncated)
}

func TestFilteredLogRingMemoryDoesNotScaleWithRequestedLines(t *testing.T) {
	opts := &logOptions{tailLines: int(^uint(0) >> 1), maxBytes: 8}
	got, truncated, err := selectLogLines(context.Background(), strings.NewReader(strings.Repeat("a\n", 100000)), opts)
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, got, 8)
	require.Equal(t, "a\na\na\na\n", string(got))
}

func TestFilteredLogCancellationAndReadError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := selectLogLines(ctx, strings.NewReader("one\ntwo\n"), &logOptions{tailLines: 1})
	require.ErrorIs(t, err, context.Canceled)
	_, _, err = selectLogLines(context.Background(), errorReader{}, &logOptions{tailLines: 1})
	require.ErrorContains(t, err, "read failure")
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestLogFilterValidationBeforeClientResolution(t *testing.T) {
	for _, args := range [][]string{
		{"job", "1", "--tail", "0"},
		{"job", "1", "--tail", "-1"},
		{"job", "1", "--grep", "["},
		{"job", "1", "--tail", "1", "--follow"},
		{"job", "1", "--grep", "", "--follow"},
	} {
		cmd := NewCmdLog(&cmdutil.Factory{Config: func() (*config.Config, error) {
			t.Fatal("invalid filter should not resolve credentials")
			return nil, nil
		}})
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		require.Error(t, cmd.Execute())
	}
}

func logTestClient(t *testing.T, handler http.Handler) (*jenkins.Client, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", dir+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("log-test"), "test-token"))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := &config.Config{}
	cfg.SetContext("log-test", &config.Context{URL: srv.URL, Username: "test", AllowInsecureStore: true})
	require.NoError(t, cfg.SetActive("log-test"))
	client, err := jenkins.NewClient(context.Background(), cfg, "log-test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)
	return client, cfg
}

func TestLogFilteredCommandJSONUsesCompleteConsole(t *testing.T) {
	var paths []string
	client, cfg := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/job/folder/job/main/7/api/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"building":false,"result":"FAILURE"}`)
		case "/job/folder/job/main/7/consoleText":
			_, _ = io.WriteString(w, strings.Repeat("prefix\n", 400000)+"FAIL one\nFAIL two")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return cfg, nil }, JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) { return client, nil }}
	cmd := NewCmdLog(f)
	cmd.PersistentFlags().Bool("json", false, "JSON")
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"folder/main", "7", "--grep", "^FAIL", "--tail", "1", "--json"})
	require.NoError(t, cmd.Execute())
	var output logOutput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &output))
	require.Equal(t, "FAIL two", output.Log)
	require.True(t, output.Complete)
	require.False(t, output.Truncated)
	require.Equal(t, []string{"/job/folder/job/main/7/api/json", "/job/folder/job/main/7/consoleText"}, paths)

	plainCmd := NewCmdLog(f)
	var plain bytes.Buffer
	plainCmd.SetOut(&plain)
	plainCmd.SetArgs([]string{"folder/main", "7", "--grep", "^FAIL", "--tail", "1", "--plain"})
	require.NoError(t, plainCmd.Execute())
	require.Equal(t, "FAIL two\n", plain.String())
}

func TestLogFiltersRejectRunningBuild(t *testing.T) {
	client, cfg := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/job/main/7/api/json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"building":true}`)
	}))
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return cfg, nil }, JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) { return client, nil }}
	cmd := NewCmdLog(f)
	cmd.SetArgs([]string{"main", "7", "--tail", "1"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.ErrorContains(t, cmd.Execute(), "require a completed build")
}

func TestSnapshotSingleHugeResponseHonorsHardBudget(t *testing.T) {
	for _, more := range []bool{false, true} {
		t.Run(fmt.Sprint(more), func(t *testing.T) {
			client, _ := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/job/main/7/logText/progressiveText", r.URL.Path)
				w.Header().Set("X-More-Data", fmt.Sprint(more))
				_, _ = io.Copy(w, io.LimitReader(zeroReader{}, 3*1024*1024))
			}))
			var out bytes.Buffer
			truncated, err := shared.CollectLogSnapshot(context.Background(), client, "main", 7, 2*1024*1024, &out)
			require.NoError(t, err)
			require.True(t, truncated)
			require.Len(t, out.Bytes(), 2*1024*1024)
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }

func TestSnapshotExactBudgetAndProgressiveOffsets(t *testing.T) {
	for _, budget := range []int{6, 11, 20} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			requests := 0
			client, _ := logTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Query().Get("start") == "0" {
					w.Header().Set("X-More-Data", "true")
					w.Header().Set("X-Text-Size", "6")
					_, _ = io.WriteString(w, "first\n")
					return
				}
				require.Equal(t, "6", r.URL.Query().Get("start"))
				_, _ = io.WriteString(w, "last\n")
			}))
			var out bytes.Buffer
			truncated, err := shared.CollectLogSnapshot(context.Background(), client, "main", 7, budget, &out)
			require.NoError(t, err)
			if budget == 6 {
				require.True(t, truncated)
				require.Equal(t, "first\n", out.String())
				require.Equal(t, 1, requests)
			} else {
				require.False(t, truncated)
				require.Equal(t, "first\nlast\n", out.String())
				require.Equal(t, 2, requests)
			}
		})
	}
}
