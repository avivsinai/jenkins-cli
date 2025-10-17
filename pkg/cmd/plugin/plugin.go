package plugin

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type pluginListResponse struct {
	Plugins []struct {
		ShortName string `json:"shortName"`
		Version   string `json:"version"`
		Enabled   bool   `json:"enabled"`
		Pinned    bool   `json:"pinned"`
	} `json:"plugins"`
}

func NewCmdPlugin(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Inspect and manage Jenkins plugins",
	}

	cmd.AddCommand(
		newPluginListCmd(f),
		newPluginInstallCmd(f),
		newPluginToggleCmd(f, true),
		newPluginToggleCmd(f, false),
	)
	return cmd
}

func newPluginListCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			var resp pluginListResponse
			_, err = client.Do(client.NewRequest().SetQueryParam("depth", "1"), http.MethodGet, "/pluginManager/api/json", &resp)
			if err != nil {
				return err
			}

			type pluginRow struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Enabled bool   `json:"enabled"`
				Pinned  bool   `json:"pinned"`
			}

			rows := make([]pluginRow, 0, len(resp.Plugins))
			for _, p := range resp.Plugins {
				rows = append(rows, pluginRow{
					Name:    p.ShortName,
					Version: p.Version,
					Enabled: p.Enabled,
					Pinned:  p.Pinned,
				})
			}

			return shared.PrintOutput(cmd, rows, func() error {
				if len(rows) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins installed")
					return nil
				}
				for _, row := range rows {
					status := "enabled"
					if !row.Enabled {
						status = "disabled"
					}
					if row.Pinned {
						status += " (pinned)"
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", row.Name, row.Version, status)
				}
				return nil
			})
		},
	}
}

func newPluginInstallCmd(f *cmdutil.Factory) *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "install <plugin[@version]> [<plugin[@version]>...]",
		Short: "Install plugins via the Jenkins update center",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !assumeYes {
				ios, err := f.Streams()
				if err != nil {
					return err
				}
				if !ios.IsStdinTTY() {
					return errors.New("confirmation required when stdin is not a TTY (use --yes)")
				}
				_, _ = fmt.Fprintf(ios.ErrOut, "Install plugins: %s? [y/N]: ", strings.Join(args, ", "))
				reader := bufio.NewReader(ios.In)
				answer, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
					return err
				}
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Cancelled")
					return cmdutil.ErrSilent
				}
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			payload, err := buildInstallXML(args)
			if err != nil {
				return err
			}

			req := client.NewRequest().SetBody(payload).SetHeader("Content-Type", "text/xml")
			resp, err := client.Do(req, http.MethodPost, "/pluginManager/installNecessaryPlugins", nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("install failed: %s", resp.Status())
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Plugin installation triggered. Monitor Jenkins for progress.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Do not prompt for confirmation")
	return cmd
}

type installRequest struct {
	XMLName xml.Name       `xml:"jenkins"`
	Install []installEntry `xml:"install"`
}

type installEntry struct {
	Plugin string `xml:"plugin,attr"`
}

func buildInstallXML(plugins []string) ([]byte, error) {
	if len(plugins) == 0 {
		return nil, errors.New("at least one plugin required")
	}
	req := installRequest{}
	for _, p := range plugins {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(trimmed, "@") {
			trimmed += "@latest"
		}
		req.Install = append(req.Install, installEntry{Plugin: trimmed})
	}
	if len(req.Install) == 0 {
		return nil, errors.New("no valid plugin identifiers provided")
	}
	buf := &bytes.Buffer{}
	enc := xml.NewEncoder(buf)
	enc.Indent("", "  ")
	if err := enc.Encode(req); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newPluginToggleCmd(f *cmdutil.Factory, enable bool) *cobra.Command {
	verb := "enable"
	help := "Enable a plugin"
	if !enable {
		verb = "disable"
		help = "Disable a plugin"
	}

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s <name>", verb),
		Short: help,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return errors.New("plugin name required")
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/pluginManager/plugin/%s/%s", url.PathEscape(name), verb)
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("%s failed: %s", verb, resp.Status())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Plugin %s %sd\n", name, verb)
			return nil
		},
	}
	return cmd
}
