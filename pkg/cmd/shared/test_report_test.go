package shared_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/run"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	testcmd "github.com/avivsinai/jenkins-cli/pkg/cmd/test"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func TestTestReportCountFallback(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		want          int
	}{
		{"mixed", `{"passCount":3,"failCount":2,"skipCount":1}`, 6},
		{"all pass", `{"passCount":3}`, 3},
		{"all fail", `{"failCount":2}`, 2},
		{"skipped", `{"skipCount":4}`, 4},
		{"empty", `{}`, 0},
		{"explicit total", `{"totalCount":8,"passCount":3,"failCount":2}`, 8},
		{"explicit zero", `{"totalCount":0,"passCount":3}`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var report shared.TestReport
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &report))
			require.Equal(t, tt.want, report.TotalCount)
		})
	}

	// Decoding into a reused value must not retain fields from an older report.
	report := shared.TestReport{PassCount: 100, TotalCount: 100}
	require.NoError(t, json.Unmarshal([]byte(`{"failCount":1}`), &report))
	require.Equal(t, 1, report.TotalCount)
	require.Zero(t, report.PassCount)
}

func TestTestReportFailureFieldsAndFiltering(t *testing.T) {
	var report shared.TestReport
	require.NoError(t, json.Unmarshal([]byte(`{"passCount":1,"failCount":2,"skipCount":1,"suites":[
		{"name":"mixed","cases":[
			{"className":"example.Calculator","name":"test_sum","status":"FAILED","duration":0.1,"errorDetails":"expected 4, got 5","errorStackTrace":"assertion\n  at Calculator:41","failedSince":41,"age":2},
			{"className":"example.Calculator","name":"test_other","status":"REGRESSION"},
			{"name":"fixed","status":"FIXED"}]},
		{"name":"passing","cases":[{"name":"pass","status":"PASSED"},{"name":"skip","status":"SKIPPED"}]}]}`), &report))
	filtered := report.FailedOnly()
	require.Equal(t, 4, filtered.TotalCount)
	require.Equal(t, 1, filtered.PassCount)
	require.Equal(t, 2, filtered.FailCount)
	require.Equal(t, 1, filtered.SkipCount)
	require.Len(t, filtered.Suites, 1)
	require.Len(t, filtered.Suites[0].Cases, 2)
	require.Len(t, report.Suites, 2)
	require.Len(t, report.Suites[0].Cases, 3)

	encoded, err := json.Marshal(filtered)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"errorDetails":"expected 4, got 5"`)
	require.Contains(t, string(encoded), `"errorStackTrace":"assertion\n  at Calculator:41"`)
	require.Contains(t, string(encoded), `"failedSince":41`)
	require.Contains(t, string(encoded), `"age":2`)
	require.Contains(t, string(encoded), `"duration":0.1`)
	require.NotContains(t, string(encoded), `"name":"fixed"`)

	filtered = (&shared.TestReport{PassCount: 1, TotalCount: 1, Suites: []shared.TestSuite{{Name: "pass", Cases: []shared.TestCase{{Status: "PASSED"}}}}}).FailedOnly()
	require.Empty(t, filtered.Suites)
	require.NotNil(t, filtered.Suites)
}

func junitFactory(t *testing.T, reportJSON string, status int) (*cmdutil.Factory, *jenkins.Client) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("JK_CONTEXT", "")
	t.Setenv("JK_TOKEN", "")
	t.Setenv("JK_URL", "")
	t.Setenv("JK_USER", "")
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")
	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("junit"), "token"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/job/app/job/main/42/api/json":
			_, _ = fmt.Fprint(w, `{"number":42,"result":"UNSTABLE","duration":12000,"url":"https://jenkins.example/app/42/"}`)
		case "/job/app/job/main/42/testReport/api/json":
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, reportJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{}
	cfg.SetContext("junit", &config.Context{URL: srv.URL, Username: "alice", AllowInsecureStore: true})
	require.NoError(t, cfg.SetActive("junit"))
	client, err := jenkins.NewClient(context.Background(), cfg, "junit", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)
	return &cmdutil.Factory{
		Config: func() (*config.Config, error) { return cfg, nil },
		JenkinsClient: func(context.Context, string, ...jenkins.ClientOption) (*jenkins.Client, error) {
			return client, nil
		},
	}, client
}

func junitCommand(t *testing.T, f *cmdutil.Factory, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "jk", SilenceUsage: true}
	root.PersistentFlags().String("context", "", "")
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("yaml", false, "")
	root.PersistentFlags().String("format", "", "")
	root.AddCommand(testcmd.NewCmdTest(f), run.NewCmdRun(f))
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func TestJUnitReportCommands(t *testing.T) {
	const payload = `{"passCount":1,"failCount":2,"skipCount":1,"suites":[{"name":"suite","cases":[
		{"className":"Calc","name":"pass","status":"PASSED"},
		{"className":"Calc","name":"first","status":"REGRESSION","errorDetails":"expected four","failedSince":42,"age":1},
		{"className":"Calc","name":"repeated","status":"FAILED","errorStackTrace":"full trace","failedSince":41,"age":2},
		{"className":"Calc","name":"skip","status":"SKIPPED"}]}]}`
	for _, format := range []string{"human", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			f, _ := junitFactory(t, payload, http.StatusOK)
			args := []string{"test", "report", "app/main", "42", "--failed"}
			if format != "human" {
				args = append(args, "--"+format)
			}
			out, err := junitCommand(t, f, args...)
			require.NoError(t, err)
			require.NotContains(t, out, `"pass"`)
			require.NotContains(t, out, "Calc.pass")
			require.NotContains(t, out, "PASSED")
			require.NotContains(t, out, "SKIPPED")
			require.Contains(t, out, "REGRESSION")
			require.Contains(t, out, "FAILED")
			switch format {
			case "json":
				var report shared.TestReport
				require.NoError(t, json.Unmarshal([]byte(out), &report))
				require.Equal(t, 4, report.TotalCount)
				require.Equal(t, 1, report.PassCount)
				require.Equal(t, 2, report.FailCount)
				require.Equal(t, 1, report.SkipCount)
				require.Len(t, report.Suites[0].Cases, 2)
				require.Equal(t, "full trace", report.Suites[0].Cases[1].ErrorStackTrace)
			case "human":
				require.Contains(t, out, "Total: 4\nFailed: 2\nSkipped: 1\n")
				require.Contains(t, out, "expected four")
			}
		})
	}
}

func TestRunSummaryFailureDetailsAreBounded(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	cases := make([]shared.TestCase, 7)
	for i := range cases {
		cases[i] = shared.TestCase{ClassName: "Calc", Name: fmt.Sprintf("failed_%d", i), Status: "FAILED", ErrorDetails: strings.Repeat("界", 500) + "\nnext line", ErrorStackTrace: "TRACE_MUST_NOT_APPEAR", FailedSince: 41, Age: 2}
	}
	cases[0].Status = "REGRESSION"
	data, err := json.Marshal(map[string]any{"passCount": 2, "failCount": 7, "suites": []shared.TestSuite{{Name: "suite", Cases: cases}}})
	require.NoError(t, err)
	f, _ := junitFactory(t, string(data), http.StatusOK)
	out, err := junitCommand(t, f, "run", "view", "app/main", "42", "--summary")
	require.NoError(t, err)
	require.Contains(t, out, "9 total, 2 passed, 7 failed, 0 skipped")
	require.Contains(t, out, "Calc.failed_0 (since #41, age 2)")
	require.Contains(t, out, "Calc.failed_4")
	require.NotContains(t, out, "Calc.failed_5")
	require.Contains(t, out, "2 more failed cases")
	require.NotContains(t, out, "TRACE_MUST_NOT_APPEAR")
	require.NotContains(t, out, "next line")
	require.Contains(t, out, strings.Repeat("界", 240)+"...")
	for _, line := range strings.Split(out, "\n") {
		require.LessOrEqual(t, len([]rune(line)), 250)
	}
}

func TestMissingJUnitReportBehavior(t *testing.T) {
	f, client := junitFactory(t, `{"message":"not found"}`, http.StatusNotFound)
	report, err := shared.FetchTestReport(client, "app/main", 42)
	require.NoError(t, err)
	require.Nil(t, report)
	out, err := junitCommand(t, f, "test", "report", "app/main", "42", "--failed")
	require.NoError(t, err)
	require.Equal(t, "No test report available\n", out)
	out, err = junitCommand(t, f, "run", "view", "app/main", "42", "--summary")
	require.NoError(t, err)
	require.NotContains(t, out, "Tests:")
	require.NotContains(t, out, "Failed tests:")
}

func TestRunJUnitSummaryWithoutFailureDetailsAndJSONCompatibility(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	f, _ := junitFactory(t, `{"passCount":1,"failCount":1,"suites":[{"cases":[{"name":"minimal_failure","status":"FAILED"}]}]}`, http.StatusOK)
	out, err := junitCommand(t, f, "run", "view", "app/main", "42", "--summary")
	require.NoError(t, err)
	require.Contains(t, out, "2 total, 1 passed, 1 failed, 0 skipped")
	require.Contains(t, out, "  minimal_failure\n")
	require.NotContains(t, out, "since #")

	out, err = junitCommand(t, f, "run", "view", "app/main", "42", "--json")
	require.NoError(t, err)
	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.JSONEq(t, `{"total":2,"failed":1,"skipped":0}`, string(result["tests"]))
	require.NotContains(t, out, "minimal_failure")
}

func TestFailedReportHumanTextIsSafeAndBounded(t *testing.T) {
	name := "clipboard\x1b]52;c;c2VjcmV0\a\n" + strings.Repeat("界", 300)
	details := "\x1b[2J\x1b[Hclear screen\r\n" + strings.Repeat("甲", 2000)
	payload, err := json.Marshal(shared.TestReport{FailCount: 1, TotalCount: 1, Suites: []shared.TestSuite{{Cases: []shared.TestCase{{Name: name, Status: "\nFAILED\n", ErrorDetails: details}}}}})
	require.NoError(t, err)
	f, _ := junitFactory(t, string(payload), http.StatusOK)
	out, err := junitCommand(t, f, "test", "report", "app/main", "42", "--failed")
	require.NoError(t, err)
	require.NotContains(t, out, "\x1b")
	require.NotContains(t, out, "\a")
	require.NotContains(t, out, "\r")
	require.Contains(t, out, "(FAILED)")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 6)
	require.LessOrEqual(t, len([]rune(lines[4])), 160+3+len(" (FAILED)"))
	require.LessOrEqual(t, len([]rune(lines[5])), 2+1024+3)

	// Machine-readable reports retain the precise data, including control bytes.
	out, err = junitCommand(t, f, "test", "report", "app/main", "42", "--failed", "--json")
	require.NoError(t, err)
	var report shared.TestReport
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.Equal(t, name, report.Suites[0].Cases[0].Name)
	require.Equal(t, details, report.Suites[0].Cases[0].ErrorDetails)
}
