package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/config"
	jklog "github.com/your-org/jenkins-cli/internal/log"
)

// Execute runs the root command and exits non-zero on failure.
func Execute() {
	root := NewRootCmd()
	if err := root.Execute(); err != nil {
		var exitErr *exitError
		if errors.As(err, &exitErr) {
			if exitErr.msg != "" {
				fmt.Fprintln(os.Stderr, exitErr.msg)
			}
			os.Exit(exitErr.code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// NewRootCmd builds the top-level jk Cobra command tree.
func NewRootCmd() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:           "jk",
		Short:         "jk is the Jenkins CLI for developers",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			jklog.Configure("", cmd.ErrOrStderr())

			if opts.cfg == nil {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				opts.cfg = cfg
			}

			ctx := context.WithValue(cmd.Root().Context(), configContextKey{}, opts.cfg)
			cmd.Root().SetContext(ctx)

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			// Default command prints help when no subcommand supplied.
			cmd.Help() //nolint:errcheck // help text failure is non-critical
		},
	}

	cmd.SetContext(context.Background())

	cmd.PersistentFlags().StringP("context", "c", "", "Active Jenkins context name")
	cmd.PersistentFlags().Bool("json", false, "Output in JSON format when supported")
	cmd.PersistentFlags().Bool("yaml", false, "Output in YAML format when supported")

	cmd.AddCommand(
		newAuthCmd(),
		newContextCmd(),
		newJobCmd(),
		newRunCmd(),
		newLogCmd(),
		newArtifactCmd(),
		newQueueCmd(),
		newTestCmd(),
	)
	cmd.AddCommand(newVersionCmd())

	return cmd
}

type rootOptions struct {
	cfg *config.Config
}

type configContextKey struct{}

// ConfigFromCmd retrieves the CLI configuration from the Cobra context.
func ConfigFromCmd(cmd *cobra.Command) *config.Config {
	if cfg, ok := cmd.Context().Value(configContextKey{}).(*config.Config); ok {
		return cfg
	}
	return nil
}
