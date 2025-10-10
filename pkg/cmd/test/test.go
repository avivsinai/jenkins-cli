package testcmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

func NewCmdTest(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Inspect test results",
	}

	cmd.AddCommand(newTestReportCmd(f))
	return cmd
}

func newTestReportCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report <jobPath> <buildNumber>",
		Short: "Show aggregated test results",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}

			report, err := shared.FetchTestReport(client, args[0], int64(num))
			if err != nil {
				return err
			}

			if report == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No test report available")
				return nil
			}

			return shared.PrintOutput(cmd, report, func() error {
				fmt.Fprintf(cmd.OutOrStdout(), "Total: %d\nFailed: %d\nSkipped: %d\n", report.TotalCount, report.FailCount, report.SkipCount)
				if len(report.Suites) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Suites: %d\n", len(report.Suites))
				}
				return nil
			})
		},
	}

	return cmd
}
