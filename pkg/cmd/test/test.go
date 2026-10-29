package testcmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
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
	var failedOnly bool
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
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No test report available")
				return nil
			}
			if failedOnly {
				report = report.FailedOnly()
			}

			return shared.PrintOutput(cmd, report, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Total: %d\nFailed: %d\nSkipped: %d\n", report.TotalCount, report.FailCount, report.SkipCount)
				if len(report.Suites) > 0 {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Suites: %d\n", len(report.Suites))
				}
				if failedOnly {
					for _, suite := range report.Suites {
						for _, c := range suite.Cases {
							name := c.Name
							if c.ClassName != "" {
								name = c.ClassName + "." + name
							}
							_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s (%s", shared.BoundedHumanText(name, 160), shared.BoundedHumanText(c.Status, 32))
							if c.FailedSince > 0 {
								_, _ = fmt.Fprintf(cmd.OutOrStdout(), "; since #%d", c.FailedSince)
							}
							if c.Age > 0 {
								_, _ = fmt.Fprintf(cmd.OutOrStdout(), "; age %d", c.Age)
							}
							_, _ = fmt.Fprintln(cmd.OutOrStdout(), ")")
							if c.ErrorDetails != "" {
								_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", shared.BoundedHumanText(c.ErrorDetails, 1024))
							}
						}
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&failedOnly, "failed", false, "Show only FAILED and REGRESSION cases; retain overall counts")

	return cmd
}
