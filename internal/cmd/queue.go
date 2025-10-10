package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
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

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Inspect the build queue",
	}

	cmd.AddCommand(newQueueListCmd(), newQueueCancelCmd())
	return cmd
}

func newQueueListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List queued items",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			var resp queueListResponse
			_, err = client.Do(client.NewRequest().SetQueryParam("tree", "items[id,task[name,url],why,inQueueSince]"), http.MethodGet, "/queue/api/json", &resp)
			if err != nil {
				return err
			}

			return printOutput(cmd, resp.Items, func() error {
				if len(resp.Items) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Queue is empty")
					return nil
				}
				for _, item := range resp.Items {
					wait := time.Since(time.UnixMilli(item.InQueueSince))
					fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\twaiting %s\t%s\n", item.ID, item.Task.Name, wait.Truncate(time.Second), item.Why)
				}
				return nil
			})
		},
	}
}

func newQueueCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a queued item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/queue/cancelItem?id=%s", args[0])
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("cancel failed: %s", resp.Status())
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Cancelled queue item %s\n", args[0])
			return nil
		},
	}
}
