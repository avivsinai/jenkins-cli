package job

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/jenkins"
	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

type jobListResponse struct {
	Jobs []jobSummary `json:"jobs"`
}

type jobSummary struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}

func NewCmdJob(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Manage Jenkins jobs and pipelines",
	}

	cmd.AddCommand(
		newJobListCmd(f),
		newJobViewCmd(f),
	)

	return cmd
}

func newJobListCmd(f *cmdutil.Factory) *cobra.Command {
	var folder string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			path := "/api/json"
			if folder != "" {
				path = fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(folder))
			}

			var resp jobListResponse
			_, err = client.Do(
				client.NewRequest().
					SetQueryParam("tree", "jobs[name,url,color]"),
				"GET",
				path,
				&resp,
			)
			if err != nil {
				return err
			}

			sort.Slice(resp.Jobs, func(i, j int) bool {
				return resp.Jobs[i].Name < resp.Jobs[j].Name
			})

			return shared.PrintOutput(cmd, resp.Jobs, func() error {
				if len(resp.Jobs) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No jobs found")
					return nil
				}
				for _, job := range resp.Jobs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", job.Name, job.URL)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Folder path to list jobs from")
	return cmd
}

func newJobViewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <jobPath>",
		Short: "View job details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			jobPath := fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(args[0]))

			var data map[string]any
			_, err = client.Do(client.NewRequest(), "GET", jobPath, &data)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, data, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Name: %v\n", data["name"])
				if desc, ok := data["description"].(string); ok && desc != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", desc)
				}
				if url, ok := data["url"].(string); ok {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", url)
				}
				return nil
			})
		},
	}

	return cmd
}
