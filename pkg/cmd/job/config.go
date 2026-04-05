package job

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type jobConfigureResult struct {
	Path       string `json:"path" yaml:"path"`
	URL        string `json:"url,omitempty" yaml:"url,omitempty"`
	Mode       string `json:"mode" yaml:"mode"`
	Source     string `json:"source,omitempty" yaml:"source,omitempty"`
	ScriptPath string `json:"scriptPath,omitempty" yaml:"scriptPath,omitempty"`
}

type jobScanResult struct {
	Path          string `json:"path" yaml:"path"`
	URL           string `json:"url,omitempty" yaml:"url,omitempty"`
	Endpoint      string `json:"endpoint" yaml:"endpoint"`
	QueueLocation string `json:"queueLocation,omitempty" yaml:"queueLocation,omitempty"`
}

func newJobConfigCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <jobPath>",
		Short: "Fetch a job's raw config.xml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.ValidateOutputFlags(cmd); err != nil {
				return err
			}
			if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
				return errors.New("job config only supports raw XML output for now")
			}

			jobPath := strings.TrimSpace(args[0])

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			configXML, err := fetchJobConfigXML(cmd.Context(), client, jobPath)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprint(cmd.OutOrStdout(), configXML)
			return nil
		},
	}

	return cmd
}

func newJobConfigureCmd(f *cmdutil.Factory) *cobra.Command {
	var file string
	var stdin bool
	var scriptPath string

	cmd := &cobra.Command{
		Use:   "configure <jobPath>",
		Short: "Update a job's config.xml",
		Long: "Update a job's config.xml.\n\n" +
			"Use `--file` or `--stdin` to post a full raw XML replacement, or use\n" +
			"`--script-path` to fetch, patch, and post a Multibranch Pipeline config.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.ValidateOutputFlags(cmd); err != nil {
				return err
			}

			jobPath := strings.TrimSpace(args[0])
			changeScriptPath := cmd.Flags().Changed("script-path")
			rawMode := file != "" || stdin

			switch {
			case rawMode && changeScriptPath:
				return errors.New("cannot combine raw XML replacement with --script-path")
			case !rawMode && !changeScriptPath:
				return errors.New("provide --file, --stdin, or --script-path")
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			result := jobConfigureResult{
				Path: jobPath,
				URL:  jobURL(client, jobPath),
			}

			if rawMode {
				configXML, source, err := readConfigXMLInput(cmd, file, stdin)
				if err != nil {
					return err
				}
				if err := updateJobConfigXML(cmd.Context(), client, jobPath, configXML); err != nil {
					return err
				}

				result.Mode = "raw"
				result.Source = source
			} else {
				if strings.TrimSpace(scriptPath) == "" {
					return errors.New("--script-path cannot be empty")
				}

				configXML, err := fetchJobConfigXML(cmd.Context(), client, jobPath)
				if err != nil {
					return err
				}
				configXML, err = updateScriptPathInConfig(configXML, scriptPath)
				if err != nil {
					return fmt.Errorf("update script path for %s: %w", jobPath, err)
				}
				if err := updateJobConfigXML(cmd.Context(), client, jobPath, configXML); err != nil {
					return err
				}

				result.Mode = "script-path"
				result.ScriptPath = scriptPath
			}

			return shared.PrintOutput(cmd, result, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", jobPath)
				if result.Mode == "raw" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Source: %s\n", result.Source)
				}
				if result.ScriptPath != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Script Path: %s\n", result.ScriptPath)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Read a full config.xml replacement from a file")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read a full config.xml replacement from standard input")
	cmd.Flags().StringVar(&scriptPath, "script-path", "", "Patch the Multibranch Pipeline Jenkinsfile path")

	return cmd
}

func newJobScanCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan <jobPath>",
		Short: "Trigger a Multibranch Pipeline rescan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.ValidateOutputFlags(cmd); err != nil {
				return err
			}

			jobPath := strings.TrimSpace(args[0])

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			if err := requireMultibranchJob(cmd.Context(), client, jobPath); err != nil {
				return err
			}

			resp, err := triggerJobScan(cmd.Context(), client, jobPath)
			if err != nil {
				return err
			}

			result := jobScanResult{
				Path:          jobPath,
				URL:           jobURL(client, jobPath),
				Endpoint:      "build",
				QueueLocation: resp.Header().Get("Location"),
			}

			return shared.PrintOutput(cmd, result, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered scan for %s\n", jobPath)
				if result.QueueLocation != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Queue: %s\n", result.QueueLocation)
				}
				return nil
			})
		},
	}

	return cmd
}

func jobConfigPath(jobPath string) (string, error) {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return "", errors.New("job path is required")
	}
	return fmt.Sprintf("/%s/config.xml", encoded), nil
}

func fetchJobConfigXML(ctx context.Context, client *jenkins.Client, jobPath string) (string, error) {
	path, err := jobConfigPath(jobPath)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetHeader("Accept", "application/xml"),
		http.MethodGet,
		path,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("fetch config.xml for %s: %w", jobPath, err)
	}
	if resp.StatusCode() >= 400 {
		return "", responseStatusError(fmt.Sprintf("fetch config.xml for %s", jobPath), resp.Status(), resp.String())
	}
	return resp.String(), nil
}

func updateJobConfigXML(ctx context.Context, client *jenkins.Client, jobPath, configXML string) error {
	path, err := jobConfigPath(jobPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(configXML) == "" {
		return errors.New("config.xml payload cannot be empty")
	}

	resp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetHeader("Accept", "application/xml").
			SetHeader("Content-Type", "application/xml").
			SetBody(configXML),
		http.MethodPost,
		path,
		nil,
	)
	if err != nil {
		return fmt.Errorf("update config.xml for %s: %w", jobPath, err)
	}
	if resp.StatusCode() >= 400 {
		return responseStatusError(fmt.Sprintf("update config.xml for %s", jobPath), resp.Status(), resp.String())
	}
	return nil
}

func readConfigXMLInput(cmd *cobra.Command, file string, stdin bool) (string, string, error) {
	switch {
	case file != "" && stdin:
		return "", "", errors.New("cannot use --file with --stdin")
	case file == "" && !stdin:
		return "", "", errors.New("provide --file or --stdin")
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", "", fmt.Errorf("read config.xml from %s: %w", file, err)
		}
		return string(data), file, nil
	default:
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", "", fmt.Errorf("read config.xml from stdin: %w", err)
		}
		return string(data), "stdin", nil
	}
}

const multiBranchClass = "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"

type jobTypeResponse struct {
	Class string `json:"_class"`
}

func requireMultibranchJob(ctx context.Context, client *jenkins.Client, jobPath string) error {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return errors.New("job path is required")
	}

	var resp jobTypeResponse
	_, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetQueryParam("tree", "_class"),
		http.MethodGet,
		fmt.Sprintf("/%s/api/json", encoded),
		&resp,
	)
	if err != nil {
		return fmt.Errorf("check job type for %s: %w", jobPath, err)
	}

	if resp.Class != multiBranchClass {
		return fmt.Errorf("%s is not a Multibranch Pipeline (type: %s); scan only applies to Multibranch jobs", jobPath, resp.Class)
	}
	return nil
}

func triggerJobScan(ctx context.Context, client *jenkins.Client, jobPath string) (*resty.Response, error) {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return nil, errors.New("job path is required")
	}

	resp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetHeader("Content-Type", "application/x-www-form-urlencoded").
			SetQueryParam("delay", "0"),
		http.MethodPost,
		fmt.Sprintf("/%s/build", encoded),
		nil,
	)
	if err != nil {
		return nil, err
	}
	if err := validateJobScanResponse(jobPath, resp.StatusCode(), resp.Status(), resp.String()); err != nil {
		return nil, err
	}
	return resp, nil
}

func validateJobScanResponse(jobPath string, statusCode int, status, body string) error {
	if statusCode < 400 {
		return nil
	}
	if statusCode == http.StatusInternalServerError &&
		strings.Contains(strings.ToLower(body), "cannot be recomputed") {
		return fmt.Errorf("%s cannot be scanned: job has no buildable sources configured", jobPath)
	}
	return responseStatusError(fmt.Sprintf("trigger scan for %s", jobPath), status, body)
}
