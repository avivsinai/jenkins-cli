package queue

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type queueListResponse struct {
	Items []queueItem `json:"items"`
}

type queueItem struct {
	ID           int64        `json:"id"`
	Why          string       `json:"why"`
	InQueueSince int64        `json:"inQueueSince"`
	Task         queueTaskRef `json:"task"`
}

type queueTaskRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func NewCmdQueue(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Inspect the build queue",
	}

	cmd.AddCommand(newQueueListCmd(f), newQueueCancelCmd(f))
	return cmd
}

func newQueueListCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List queued items",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			var resp queueListResponse
			_, err = client.Do(client.NewRequest().SetQueryParam("tree", "items[id,task[name,url],why,inQueueSince]"), http.MethodGet, "/queue/api/json", &resp)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, resp.Items, func() error {
				if len(resp.Items) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Queue is empty")
					return nil
				}
				for _, item := range resp.Items {
					wait := time.Since(time.UnixMilli(item.InQueueSince))
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\twaiting %s\t%s\n", item.ID, item.Task.Name, wait.Truncate(time.Second), item.Why)
				}
				return nil
			})
		},
	}
}

func newQueueCancelCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a queued item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := strconv.Atoi(args[0]); err != nil {
				return fmt.Errorf("invalid queue id %q: %w", args[0], err)
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			req := client.NewRequest().SetQueryParam("id", args[0])
			resp, err := client.Do(req, http.MethodPost, "/queue/cancelItem", nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("cancel failed: %s", resp.Status())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cancelled queue item %s\n", args[0])
			return nil
		},
	}
}
