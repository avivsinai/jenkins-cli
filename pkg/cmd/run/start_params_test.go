package run

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

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
