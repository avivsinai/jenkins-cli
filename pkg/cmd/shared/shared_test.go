package shared

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
)

func TestWantsQuiet(t *testing.T) {
	newRootWithChild := func() (*cobra.Command, *cobra.Command) {
		root := &cobra.Command{Use: "jk"}
		root.PersistentFlags().BoolP("quiet", "q", false, "Suppress non-essential output")
		child := &cobra.Command{Use: "run"}
		root.AddCommand(child)
		return root, child
	}

	tests := []struct {
		name  string
		setup func(*testing.T, *cobra.Command)
		want  bool
	}{
		{
			name:  "returns false by default",
			setup: nil,
			want:  false,
		},
		{
			name: "flag returns true",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				require.NoError(t, cmd.Root().PersistentFlags().Set("quiet", "true"))
			},
			want: true,
		},
		{
			name: "env var returns true",
			setup: func(t *testing.T, _ *cobra.Command) {
				t.Helper()
				t.Setenv("JK_QUIET", "1")
			},
			want: true,
		},
		{
			name: "flag takes precedence over absent env",
			setup: func(t *testing.T, cmd *cobra.Command) {
				t.Helper()
				require.NoError(t, cmd.Root().PersistentFlags().Set("quiet", "true"))
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, child := newRootWithChild()
			if tt.setup != nil {
				tt.setup(t, child)
			}
			got := WantsQuiet(child)
			require.Equal(t, tt.want, got)
		})
	}
}

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
