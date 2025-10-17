package version

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/build"
)

func NewCmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print jk version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "jk version %s", build.Version)
			if build.Commit != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\ncommit: %s", build.Commit)
			}
			if build.Date != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\ndate: %s", build.Date)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
}
