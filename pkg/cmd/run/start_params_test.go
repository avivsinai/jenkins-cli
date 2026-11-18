package run

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunStartParameterSubmission(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want url.Values
	}{
		{name: "no parameters"},
		{
			name: "scalar parameters",
			args: []string{"-p", "CONFIG=a=1,b=2", "-p", "EMPTY=", "-p", `JSON=["a","b"]`},
			want: url.Values{"CONFIG": {"a=1,b=2"}, "EMPTY": {""}, "JSON": {`["a","b"]`}},
		},
		{
			name: "scalar and list parameters",
			args: []string{"-p", "ENV=staging", "--param-list", `DOMAINS=["a","b","c"]`, "--param-list", `LABELS=["with,comma","with=equals+plus"]`},
			want: url.Values{"ENV": {"staging"}, "DOMAINS": {"a", "b", "c"}, "LABELS": {"with,comma", "with=equals+plus"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buildPath := "/job/app/build"
			if len(tc.want) > 0 {
				buildPath = "/job/app/buildWithParameters"
			}
			client, cfg := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/job/app/api/json":
					assert.Equal(t, http.MethodGet, r.Method)
					_, _ = io.WriteString(w, `{"_class":"hudson.model.FreeStyleProject"}`)
				case buildPath:
					assert.Equal(t, http.MethodPost, r.Method)
					assert.NoError(t, r.ParseForm())
					if len(tc.want) > 0 {
						assert.Equal(t, tc.want, r.PostForm)
					} else {
						assert.Empty(t, r.PostForm)
					}
					w.WriteHeader(http.StatusCreated)
				case "/crumbIssuer/api/json":
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			cmd, out, _ := newWatchTestCommand(client, cfg)
			cmd.SetArgs(append([]string{"run", "start", "app"}, tc.args...))
			require.NoError(t, cmd.Execute())
			require.Contains(t, out.String(), "Triggered run for app")
		})
	}
}

func TestRunStartRejectsInvalidParametersBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"duplicate scalar", []string{"-p", "DOMAINS=a", "-p", " DOMAINS =b"}, `duplicate parameter "DOMAINS"`},
		{"duplicate list", []string{"--param-list", `DOMAINS=["a"]`, "--param-list", `DOMAINS=["b"]`}, `duplicate parameter "DOMAINS"`},
		{"mixed duplicate", []string{"--param-list", `DOMAINS=["a"]`, "-p", "DOMAINS=b"}, `duplicate parameter "DOMAINS"`},
		{"missing assignment", []string{"-p", "DOMAINS"}, "expected NAME=VALUE"},
		{"empty name", []string{"--param-list", `=["a"]`}, "expected NAME=VALUE"},
		{"empty array", []string{"--param-list", `DOMAINS=[]`}, "expected a non-empty JSON array"},
		{"null array", []string{"--param-list", `DOMAINS=null`}, "expected a non-empty JSON array"},
		{"malformed array", []string{"--param-list", `DOMAINS=[a,b]`}, "expected a non-empty JSON array"},
		{"nonstring entry", []string{"--param-list", `DOMAINS=["a",null]`}, "expected only JSON strings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRunStartCmd(nil)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(append([]string{"app"}, tc.args...))
			require.ErrorContains(t, cmd.Execute(), tc.want)
		})
	}
}

// TestParamFlagType verifies that the actual -p/--param flag in newRunStartCmd
// uses StringArrayVarP (not StringSliceVarP). This is critical because
// StringSliceVarP incorrectly splits on commas, breaking values like SERVICES=a,b,c.
func TestParamFlagType(t *testing.T) {
	cmd := newRunStartCmd(nil)
	flag := cmd.Flags().Lookup("param")
	require.NotNil(t, flag, "param flag should exist")

	// StringArrayVarP creates a flag of type "stringArray"
	// StringSliceVarP creates a flag of type "stringSlice"
	require.Equal(t, "stringArray", flag.Value.Type(),
		"param flag must use StringArrayVarP to preserve commas in values")
}

// TestParamFlagWithComma verifies that the -p/--param flag correctly handles
// values containing commas using the actual command from newRunStartCmd.
func TestParamFlagWithComma(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string // expected raw flag values
	}{
		{
			name:     "single param without comma",
			args:     []string{"-p", "VERSION=1.0.0"},
			expected: []string{"VERSION=1.0.0"},
		},
		{
			name:     "single param with comma in value",
			args:     []string{"-p", "SERVICES=backstage,selenium-debug-chrome"},
			expected: []string{"SERVICES=backstage,selenium-debug-chrome"},
		},
		{
			name: "multiple params with comma in one value",
			args: []string{
				"-p", "VERSION=1.0.0",
				"-p", "SERVICES=backstage,selenium-debug-chrome",
				"-p", "DURATION=5",
			},
			expected: []string{"VERSION=1.0.0", "SERVICES=backstage,selenium-debug-chrome", "DURATION=5"},
		},
		{
			name:     "param with multiple commas",
			args:     []string{"-p", "LIST=a,b,c,d,e"},
			expected: []string{"LIST=a,b,c,d,e"},
		},
		{
			name:     "param with equals sign in value",
			args:     []string{"-p", "QUERY=key=value"},
			expected: []string{"QUERY=key=value"},
		},
		{
			name:     "param with comma and equals in value",
			args:     []string{"-p", "CONFIG=a=1,b=2,c=3"},
			expected: []string{"CONFIG=a=1,b=2,c=3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Use the actual command from newRunStartCmd
			cmd := newRunStartCmd(nil)

			// Add a dummy job arg (required by the command)
			fullArgs := make([]string, 0, len(tc.args)+1)
			fullArgs = append(fullArgs, tc.args...)
			fullArgs = append(fullArgs, "dummy/job")
			cmd.SetArgs(fullArgs)

			// Parse flags only (don't execute RunE which needs a real client)
			err := cmd.ParseFlags(fullArgs)
			require.NoError(t, err)

			// Get the parsed param values via GetStringArray
			flag := cmd.Flags().Lookup("param")
			require.NotNil(t, flag)

			// Extract actual values from the flag
			got, err := cmd.Flags().GetStringArray("param")
			require.NoError(t, err)

			require.Equal(t, tc.expected, got, "param values should match exactly")
		})
	}
}

// TestParamFlagCommaNotSplit ensures that a single -p flag with comma
// is NOT split into multiple params (the old buggy behavior).
func TestParamFlagCommaNotSplit(t *testing.T) {
	cmd := newRunStartCmd(nil)

	// Pass a single -p flag with comma in value
	args := []string{"-p", "SERVICES=svc1,svc2,svc3", "dummy/job"}
	cmd.SetArgs(args)
	err := cmd.ParseFlags(args)
	require.NoError(t, err)

	got, err := cmd.Flags().GetStringArray("param")
	require.NoError(t, err)

	// With StringArrayVarP, we should get exactly 1 param, not 3
	require.Len(t, got, 1, "comma should not split params when using StringArrayVarP")
	require.Equal(t, "SERVICES=svc1,svc2,svc3", got[0])
}

// TestParamFlagRegressionGuard guards against accidentally reverting to StringSliceVarP.
// If someone changes the flag type, this test will fail.
func TestParamFlagRegressionGuard(t *testing.T) {
	cmd := newRunStartCmd(nil)

	// This input would produce 3 params with StringSliceVarP but 1 with StringArrayVarP
	args := []string{"-p", "CSV=a,b,c", "dummy/job"}
	cmd.SetArgs(args)
	err := cmd.ParseFlags(args)
	require.NoError(t, err)

	// Try to get as StringSlice - this would work with StringSliceVarP
	// but the values would be split incorrectly
	flag := cmd.Flags().Lookup("param")
	require.NotNil(t, flag)

	// Verify it's stringArray, not stringSlice
	require.Equal(t, "stringArray", flag.Value.Type())

	// Verify we get exactly 1 value, not 3
	if sa, ok := flag.Value.(pflag.SliceValue); ok {
		vals := sa.GetSlice()
		require.Len(t, vals, 1, "with StringArrayVarP, CSV=a,b,c should be 1 param not 3")
		require.Equal(t, "CSV=a,b,c", vals[0])
	}
}
