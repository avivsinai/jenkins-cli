package shared

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/your-org/jenkins-cli/internal/config"
)

func TestResolveContextNamePrecedence(t *testing.T) {
	newConfig := func() *config.Config {
		return &config.Config{
			Active: "active",
			Contexts: map[string]*config.Context{
				"active": {
					URL: "https://jenkins.example.com",
				},
				"other": {
					URL: "https://jenkins.other.com",
				},
			},
		}
	}

	newCommand := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().String("context", "", "")
		return cmd
	}

	tests := []struct {
		name     string
		setup    func(*testing.T, *cobra.Command)
		wantName string
	}{
		{
			name: "flag overrides env and active",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				t.Setenv("JK_CONTEXT", "env-context")
				require.NoError(t, cmd.Flags().Set("context", "  flag-context  "))
			},
			wantName: "flag-context",
		},
		{
			name: "env overrides active when flag not set",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				t.Setenv("JK_CONTEXT", " env-context ")
			},
			wantName: "env-context",
		},
		{
			name: "falls back to active context when env empty",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				t.Setenv("JK_CONTEXT", "  ")
			},
			wantName: "active",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCommand()
			cfg := newConfig()

			if tt.setup != nil {
				tt.setup(t, cmd)
			}

			got, err := ResolveContextName(cmd, cfg)
			require.NoError(t, err)
			require.Equal(t, tt.wantName, got)
		})
	}
}
