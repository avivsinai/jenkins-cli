package shared

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestValidateOutputFlags(t *testing.T) {
	type testCase struct {
		name    string
		args    []string
		wantErr string
	}

	tests := []testCase{
		{
			name:    "invalid format",
			args:    []string{"--format", "table"},
			wantErr: "invalid value for --format",
		},
		{
			name:    "json with format conflict",
			args:    []string{"--json", "--format", "json"},
			wantErr: "cannot use `--json` or `--yaml` with `--format`",
		},
		{
			name:    "jq without json",
			args:    []string{"--jq", ".number"},
			wantErr: "cannot use `--jq` without specifying `--json` or `--format json`",
		},
		{
			name:    "template without json",
			args:    []string{"--template", "{{.result}}"},
			wantErr: "cannot use `--template` without specifying `--json` or `--format json`",
		},
		{
			name: "format json with jq",
			args: []string{"--format", "json", "--jq", ".result"},
		},
		{
			name: "json with jq",
			args: []string{"--json", "--jq", ".result"},
		},
		{
			name: "format yaml",
			args: []string{"--format", "yaml"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newOutputFlagsCommand()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()

			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func newOutputFlagsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "jk",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ValidateOutputFlags(cmd)
		},
	}

	cmd.PersistentFlags().Bool("json", false, "")
	cmd.PersistentFlags().Bool("yaml", false, "")
	cmd.PersistentFlags().String("jq", "", "")
	cmd.PersistentFlags().String("template", "", "")
	cmd.PersistentFlags().String("format", "", "")

	return cmd
}
