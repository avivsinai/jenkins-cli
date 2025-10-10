package cmd

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/jenkins"
)

type testReportResponse struct {
	TotalCount int             `json:"totalCount"`
	FailCount  int             `json:"failCount"`
	SkipCount  int             `json:"skipCount"`
	Suites     []testSuiteInfo `json:"suites"`
}

type testSuiteInfo struct {
	Name  string         `json:"name"`
	Cases []testCaseInfo `json:"cases"`
}

type testCaseInfo struct {
	ClassName string  `json:"className"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Duration  float64 `json:"duration"`
}

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Inspect test results",
	}

	cmd.AddCommand(newTestReportCmd())
	return cmd
}

func newTestReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report <jobPath> <buildNumber>",
		Short: "Show aggregated test results",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			num, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/%s/%d/testReport/api/json", jenkins.EncodeJobPath(args[0]), num)
			var report testReportResponse
			_, err = client.Do(client.NewRequest(), http.MethodGet, path, &report)
			if err != nil {
				return err
			}

			return printOutput(cmd, report, func() error {
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
