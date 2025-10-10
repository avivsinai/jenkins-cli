package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/build"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print jk version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "jk version %s", build.Version)
			if build.Commit != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\ncommit: %s", build.Commit)
			}
			if build.Date != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\ndate: %s", build.Date)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
}
