package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/jenkins"
)

type jobListResponse struct {
	Jobs []jobSummary `json:"jobs"`
}

type jobSummary struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}

func newJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Manage Jenkins jobs and pipelines",
	}

	cmd.AddCommand(
		newJobListCmd(),
		newJobViewCmd(),
	)

	return cmd
}

func newJobListCmd() *cobra.Command {
	var folder string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
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

			return printOutput(cmd, resp.Jobs, func() error {
				if len(resp.Jobs) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No jobs found")
					return nil
				}
				for _, job := range resp.Jobs {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", job.Name, job.URL)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Folder path to list jobs from")
	return cmd
}

func newJobViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <jobPath>",
		Short: "View job details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			jobPath := fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(args[0]))

			var data map[string]any
			_, err = client.Do(client.NewRequest(), "GET", jobPath, &data)
			if err != nil {
				return err
			}

			return printOutput(cmd, data, func() error {
				fmt.Fprintf(cmd.OutOrStdout(), "Name: %v\n", data["name"])
				if desc, ok := data["description"].(string); ok && desc != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", desc)
				}
				if url, ok := data["url"].(string); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", url)
				}
				return nil
			})
		},
	}

	return cmd
}
