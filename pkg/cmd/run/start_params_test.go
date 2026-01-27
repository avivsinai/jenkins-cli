package run

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestParamFlagWithComma verifies that the -p/--param flag correctly handles
// values containing commas. This is important for Jenkins jobs that accept
// comma-separated lists as a single parameter value (e.g., SERVICES=a,b,c).
//
// Previously, StringSliceVarP was used which incorrectly split on commas.
// Now StringArrayVarP is used, which treats each -p flag as a single value.
func TestParamFlagWithComma(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected map[string]string
	}{
		{
			name: "single param without comma",
			args: []string{"-p", "VERSION=1.0.0"},
			expected: map[string]string{
				"VERSION": "1.0.0",
			},
		},
		{
			name: "single param with comma in value",
			args: []string{"-p", "SERVICES=backstage,selenium-debug-chrome"},
			expected: map[string]string{
				"SERVICES": "backstage,selenium-debug-chrome",
			},
		},
		{
			name: "multiple params with comma in one value",
			args: []string{
				"-p", "VERSION=1.0.0",
				"-p", "SERVICES=backstage,selenium-debug-chrome",
				"-p", "DURATION=5",
			},
			expected: map[string]string{
				"VERSION":  "1.0.0",
				"SERVICES": "backstage,selenium-debug-chrome",
				"DURATION": "5",
			},
		},
		{
			name: "param with multiple commas",
			args: []string{"-p", "LIST=a,b,c,d,e"},
			expected: map[string]string{
				"LIST": "a,b,c,d,e",
			},
		},
		{
			name: "param with equals sign in value",
			args: []string{"-p", "QUERY=key=value"},
			expected: map[string]string{
				"QUERY": "key=value",
			},
		},
		{
			name: "param with comma and equals in value",
			args: []string{"-p", "CONFIG=a=1,b=2,c=3"},
			expected: map[string]string{
				"CONFIG": "a=1,b=2,c=3",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var params []string

			cmd := &cobra.Command{
				Use: "start",
				RunE: func(cmd *cobra.Command, args []string) error {
					return nil
				},
			}

			// Use StringArrayVarP (not StringSliceVarP) to preserve commas
			cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Build parameter key=value")

			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			require.NoError(t, err)

			// Parse params into map (same logic as in run.go)
			paramMap := make(map[string]string, len(params))
			for _, p := range params {
				idx := indexOfEquals(p)
				if idx < 0 {
					t.Fatalf("invalid parameter format %q (missing =)", p)
				}
				paramMap[p[:idx]] = p[idx+1:]
			}

			require.Equal(t, tc.expected, paramMap)
		})
	}
}

// indexOfEquals returns the index of the first '=' in s, or -1 if not found.
func indexOfEquals(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}

// TestParamFlagCommaNotSplit ensures that a single -p flag with comma
// is NOT split into multiple params (the old buggy behavior).
func TestParamFlagCommaNotSplit(t *testing.T) {
	var params []string

	cmd := &cobra.Command{
		Use: "start",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Build parameter key=value")

	// Pass a single -p flag with comma in value
	cmd.SetArgs([]string{"-p", "SERVICES=svc1,svc2,svc3"})
	err := cmd.Execute()
	require.NoError(t, err)

	// With StringArrayVarP, we should get exactly 1 param, not 3
	require.Len(t, params, 1, "comma should not split params when using StringArrayVarP")
	require.Equal(t, "SERVICES=svc1,svc2,svc3", params[0])
}
