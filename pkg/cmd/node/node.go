package node

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

type nodeListResponse struct {
	Computers []struct {
		DisplayName        string `json:"displayName"`
		Offline            bool   `json:"offline"`
		TemporarilyOffline bool   `json:"temporarilyOffline"`
		OfflineCauseReason string `json:"offlineCauseReason"`
	} `json:"computer"`
}

type nodeInfo struct {
	Name      string `json:"name"`
	Offline   bool   `json:"offline"`
	Temp      bool   `json:"temporarilyOffline"`
	OfflineBy string `json:"offlineCause,omitempty"`
}

func NewCmdNode(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Inspect and manage Jenkins nodes",
	}

	cmd.AddCommand(
		newNodeListCmd(f),
		newNodeCordonCmd(f),
		newNodeUncordonCmd(f),
		newNodeDeleteCmd(f),
	)
	return cmd
}

func newNodeListCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List Jenkins nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			var resp nodeListResponse
			_, err = client.Do(
				client.NewRequest().SetQueryParam("tree", "computer[displayName,offline,temporarilyOffline,offlineCauseReason]"),
				http.MethodGet,
				"/computer/api/json",
				&resp,
			)
			if err != nil {
				return err
			}

			nodes := make([]nodeInfo, 0, len(resp.Computers))
			for _, n := range resp.Computers {
				nodes = append(nodes, nodeInfo{
					Name:      n.DisplayName,
					Offline:   n.Offline,
					Temp:      n.TemporarilyOffline,
					OfflineBy: strings.TrimSpace(n.OfflineCauseReason),
				})
			}

			return shared.PrintOutput(cmd, nodes, func() error {
				if len(nodes) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No nodes found")
					return nil
				}
				for _, n := range nodes {
					state := "online"
					if n.Offline {
						state = "offline"
					}
					if n.Temp {
						state += " (cordoned)"
					}
					if n.OfflineBy != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", n.Name, state, n.OfflineBy)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", n.Name, state)
					}
				}
				return nil
			})
		},
	}
}

func newNodeCordonCmd(f *cmdutil.Factory) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "cordon <name>",
		Short: "Mark a node temporarily offline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return toggleNode(cmd, f, args[0], true, message)
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "Offline message to display")
	return cmd
}

func newNodeUncordonCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "uncordon <name>",
		Short: "Bring a node back online",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return toggleNode(cmd, f, args[0], false, "")
		},
	}
}

func newNodeDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return errors.New("node name required")
			}
			if isBuiltInNode(name) {
				return errors.New("cannot delete the built-in node")
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/computer/%s/doDelete", encodeNodeName(name))
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("delete failed: %s", resp.Status())
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted node %s\n", name)
			return nil
		},
	}
}

func toggleNode(cmd *cobra.Command, f *cmdutil.Factory, name string, offline bool, message string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("node name required")
	}

	client, err := shared.JenkinsClient(cmd, f)
	if err != nil {
		return err
	}

	encodedName := encodeNodeName(name)
	params := url.Values{}
	if message != "" {
		params.Set("offlineMessage", message)
	}

	desired := "false"
	if offline {
		desired = "true"
	}
	params.Set("offline", desired)

	endpoint := fmt.Sprintf("/computer/%s/toggleOffline?%s", encodedName, params.Encode())
	resp, err := client.Do(client.NewRequest(), http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 300 {
		return fmt.Errorf("toggle failed: %s", resp.Status())
	}

	state := "online"
	if offline {
		state = "cordoned"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Node %s marked %s\n", name, state)
	return nil
}

func encodeNodeName(name string) string {
	trimmed := strings.TrimSpace(name)
	switch trimmed {
	case "master", "(master)", "built-in", "(built-in)":
		return "(master)"
	default:
		return url.PathEscape(trimmed)
	}
}

func isBuiltInNode(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "master", "(master)", "built-in", "(built-in)":
		return true
	default:
		return false
	}
}
