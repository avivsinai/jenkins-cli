package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/jenkins"
	jklog "github.com/your-org/jenkins-cli/internal/log"
	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

type runListResponse struct {
	Builds []runSummary `json:"builds"`
}

type runSummary struct {
	Number            int64            `json:"number"`
	Result            string           `json:"result"`
	Building          bool             `json:"building"`
	Timestamp         int64            `json:"timestamp"`
	Duration          int64            `json:"duration"`
	EstimatedDuration int64            `json:"estimatedDuration"`
	URL               string           `json:"url"`
	QueueID           int64            `json:"queueId"`
	Actions           []map[string]any `json:"actions"`
	ChangeSet         changeSet        `json:"changeSet"`
}

type runDetail struct {
	Number            int64             `json:"number"`
	Result            string            `json:"result"`
	Building          bool              `json:"building"`
	Timestamp         int64             `json:"timestamp"`
	Duration          int64             `json:"duration"`
	EstimatedDuration int64             `json:"estimatedDuration"`
	URL               string            `json:"url"`
	Actions           []map[string]any  `json:"actions"`
	Parameters        []map[string]any  `json:"parameters"`
	Stages            []map[string]any  `json:"stages"`
	ChangeSet         changeSet         `json:"changeSet"`
	Artifacts         []artifactItem    `json:"artifacts"`
	QueueID           int64             `json:"queueId"`
	BuiltOn           string            `json:"builtOn"`
	Executor          *executorMetadata `json:"executor"`
	FullDisplayName   string            `json:"fullDisplayName"`
	Description       string            `json:"description"`
}

type artifactItem struct {
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
}

type changeSet struct {
	Items []changeSetItem `json:"items"`
}

type changeSetItem struct {
	AuthorEmail string          `json:"authorEmail"`
	CommitID    string          `json:"commitId"`
	Msg         string          `json:"msg"`
	Author      changeSetAuthor `json:"author"`
}

type changeSetAuthor struct {
	FullName string `json:"fullName"`
}

type executorMetadata struct {
	Number int `json:"number"`
}

type queueItemStatus struct {
	ID           int64            `json:"id"`
	Why          string           `json:"why"`
	Cancelled    bool             `json:"cancelled"`
	InQueueSince int64            `json:"inQueueSince"`
	Executable   *queueExecutable `json:"executable"`
}

type queueExecutable struct {
	Number int64 `json:"number"`
}

func NewCmdRun(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Interact with job runs",
	}

	cmd.AddCommand(
		newRunStartCmd(f),
		newRunListCmd(f),
		newRunViewCmd(f),
		newRunCancelCmd(f),
		newRunRerunCmd(f),
	)

	return cmd
}

func newRunStartCmd(f *cmdutil.Factory) *cobra.Command {
	var params []string
	var follow bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "start <jobPath>",
		Short: "Trigger a job run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			paramMap := make(map[string]string, len(params))
			for _, p := range params {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid parameter %q", p)
				}
				paramMap[strings.TrimSpace(parts[0])] = parts[1]
			}

			resp, err := triggerBuild(client, args[0], paramMap)
			if err != nil {
				return err
			}

			if !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd) {
				fmt.Fprintf(cmd.OutOrStdout(), "Triggered run for %s\n", args[0])
			}

			if !follow {
				if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
					payload := runTriggerOutput{
						JobPath:       args[0],
						Message:       "run requested",
						QueueLocation: queueLocationFromResponse(resp),
					}
					return shared.PrintOutput(cmd, payload, func() error {
						fmt.Fprintf(cmd.OutOrStdout(), "Triggered run for %s\n", args[0])
						return nil
					})
				}
				return nil
			}

			return followTriggeredRun(cmd, client, args[0], resp, interval)
		},
	}

	cmd.Flags().StringSliceVarP(&params, "param", "p", nil, "Build parameter key=value")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the run progress until completion")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval when following runs")
	return cmd
}

func newRunListCmd(f *cmdutil.Factory) *cobra.Command {
	var limit int
	var cursor string

	cmd := &cobra.Command{
		Use:   "ls <jobPath>",
		Short: "List recent runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			encoded := jenkins.EncodeJobPath(args[0])
			path := fmt.Sprintf("/%s/api/json", encoded)

			if limit <= 0 {
				limit = 20
			}
			fetchLimit := limit + 25
			query := fmt.Sprintf(
				"builds[number,url,result,building,timestamp,duration,estimatedDuration,queueId,actions[parameters[name,value],causes[shortDescription,userId,userName,_class],lastBuiltRevision[SHA1,branch[name]],buildsByBranchName[*],remoteUrls],changeSet[items[authorEmail,author[fullName],commitId,msg]]]{,%d}",
				fetchLimit,
			)

			var resp runListResponse
			_, err = client.Do(client.NewRequest().SetQueryParam("tree", query), http.MethodGet, path, &resp)
			if err != nil {
				return err
			}

			output, err := buildRunListOutput(args[0], cursor, resp.Builds, limit)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, output, func() error {
				if len(output.Items) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No runs found")
					return nil
				}
				for _, item := range output.Items {
					fmt.Fprintf(
						cmd.OutOrStdout(),
						"#%d\t%s\t%s\t%s\n",
						item.Number,
						strings.ToUpper(item.Result),
						item.StartTime,
						shared.DurationString(item.DurationMs),
					)
				}
				if output.NextCursor != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Next cursor: %s\n", output.NextCursor)
				}
				return nil
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Number of runs to list")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Cursor for pagination (use value from previous output)")
	return cmd
}

func newRunViewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <jobPath> <buildNumber>",
		Short: "View run details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			path := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(args[0]), num)
			var detail runDetail
			_, err = client.Do(client.NewRequest(), http.MethodGet, path, &detail)
			if err != nil {
				return err
			}

			testReport, err := shared.FetchTestReport(client, args[0], num)
			if err != nil {
				jklog.L().Debug().Err(err).Msg("fetch test report failed")
			}

			output := buildRunDetailOutput(args[0], detail, testReport)

			return shared.PrintOutput(cmd, output, func() error {
				fmt.Fprintf(cmd.OutOrStdout(), "Run #%d (%s)\n", output.Number, output.Status)
				if output.Result != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Result: %s\n", output.Result)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", output.URL)
				if output.StartTime != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Started: %s\n", output.StartTime)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Duration: %s\n", shared.DurationString(output.DurationMs))
				if output.SCM != nil && (output.SCM.Branch != "" || output.SCM.Commit != "" || output.SCM.Repo != "") {
					fmt.Fprintf(cmd.OutOrStdout(), "SCM: branch=%s commit=%s repo=%s\n", output.SCM.Branch, output.SCM.Commit, output.SCM.Repo)
				}
				if len(output.Parameters) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Parameters:")
					for _, p := range output.Parameters {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s=%v\n", p.Name, p.Value)
					}
				}
				if output.Tests != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Tests: total=%d failed=%d skipped=%d\n", output.Tests.Total, output.Tests.Failed, output.Tests.Skipped)
				}
				return nil
			})
		},
	}

	return cmd
}

func newRunCancelCmd(f *cmdutil.Factory) *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "cancel <jobPath> <buildNumber>",
		Short: "Cancel a running job",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			action, err := resolveCancelAction(mode)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/%s/%d/%s", jenkins.EncodeJobPath(args[0]), num, action)
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("cancel failed: %s", resp.Status())
			}

			if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
				payload := map[string]any{
					"jobPath": args[0],
					"build":   num,
					"action":  action,
					"status":  "requested",
				}
				return shared.PrintOutput(cmd, payload, func() error {
					fmt.Fprintf(cmd.OutOrStdout(), "Cancellation requested for %s #%d (%s)\n", args[0], num, action)
					return nil
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Cancellation requested for %s #%d (%s)\n", args[0], num, action)
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "stop", "Termination mode: stop, term, or kill")
	return cmd
}

func newRunRerunCmd(f *cmdutil.Factory) *cobra.Command {
	var follow bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "rerun <jobPath> <buildNumber>",
		Short: "Rerun a job using the previous parameters",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			detail, err := fetchRunDetail(client, args[0], num)
			if err != nil {
				return err
			}

			params := collectRerunParameters(*detail)
			resp, err := triggerBuild(client, args[0], params)
			if err != nil {
				return err
			}

			if !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd) {
				fmt.Fprintf(cmd.OutOrStdout(), "Triggered rerun for %s #%d\n", args[0], num)
			}

			if !follow {
				if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
					payload := runTriggerOutput{
						JobPath:       args[0],
						Message:       "rerun requested",
						QueueLocation: queueLocationFromResponse(resp),
					}
					return shared.PrintOutput(cmd, payload, func() error {
						fmt.Fprintf(cmd.OutOrStdout(), "Triggered rerun for %s #%d\n", args[0], num)
						return nil
					})
				}
				return nil
			}

			return followTriggeredRun(cmd, client, args[0], resp, interval)
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the rerun progress until completion")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval when following runs")
	return cmd
}

func triggerBuild(client *jenkins.Client, jobPath string, params map[string]string) (*resty.Response, error) {
	if client == nil {
		return nil, errors.New("jenkins client is required")
	}

	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return nil, errors.New("job path is required")
	}

	methodPath := fmt.Sprintf("/%s/build", encoded)
	req := client.NewRequest()
	if len(params) > 0 {
		req.SetFormData(params)
		methodPath = fmt.Sprintf("/%s/buildWithParameters", encoded)
	}

	resp, err := client.Do(req, http.MethodPost, methodPath, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("trigger build failed: %s", resp.Status())
	}
	return resp, nil
}

func followTriggeredRun(cmd *cobra.Command, client *jenkins.Client, jobPath string, resp *resty.Response, interval time.Duration) error {
	queueLocation := queueLocationFromResponse(resp)
	buildNumber, err := waitForBuildNumber(client, queueLocation, 5*time.Minute)
	if err != nil {
		return err
	}

	streamLogs := !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd)
	result, err := monitorRun(cmd, client, jobPath, buildNumber, interval, streamLogs)
	if err != nil {
		return err
	}

	if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
		detail, err := fetchRunDetail(client, jobPath, buildNumber)
		if err != nil {
			return err
		}
		testReport, err := shared.FetchTestReport(client, jobPath, buildNumber)
		if err != nil {
			jklog.L().Debug().Err(err).Msg("fetch test report failed")
		}
		output := buildRunDetailOutput(jobPath, *detail, testReport)
		if err := shared.PrintOutput(cmd, output, func() error {
			fmt.Fprintf(cmd.OutOrStdout(), "Run #%d completed with status %s\n", output.Number, output.Result)
			return nil
		}); err != nil {
			return err
		}
	}

	code := exitCodeForResult(result)
	if code == 0 {
		return nil
	}
	return shared.NewExitError(code, "")
}

func queueLocationFromResponse(resp *resty.Response) string {
	if resp == nil {
		return ""
	}
	location := resp.Header().Get("Location")
	if location == "" {
		location = resp.Header().Get("X-Queue-Item")
	}
	return location
}

func fetchRunDetail(client *jenkins.Client, jobPath string, buildNumber int64) (*runDetail, error) {
	var detail runDetail
	path := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	_, err := client.Do(client.NewRequest(), http.MethodGet, path, &detail)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func resolveCancelAction(mode string) (string, error) {
	if mode == "" {
		return "stop", nil
	}
	switch strings.ToLower(mode) {
	case "stop":
		return "stop", nil
	case "term", "terminate":
		return "term", nil
	case "kill":
		return "kill", nil
	default:
		return "", fmt.Errorf("unsupported cancel mode %q", mode)
	}
}

func waitForBuildNumber(client *jenkins.Client, queueLocation string, timeout time.Duration) (int64, error) {
	if queueLocation == "" {
		return 0, errors.New("follow requested but queue location unavailable")
	}

	queueAPI := strings.TrimSpace(queueLocation)
	if !strings.Contains(queueAPI, "/api/json") {
		queueAPI = strings.TrimSuffix(queueAPI, "/") + "/api/json"
	}

	deadline := time.Now().Add(timeout)
	for {
		var status queueItemStatus
		_, err := client.Do(client.NewRequest(), http.MethodGet, queueAPI, &status)
		if err != nil {
			return 0, err
		}

		if status.Cancelled {
			if status.Why != "" {
				return 0, fmt.Errorf("queue item cancelled: %s", status.Why)
			}
			return 0, errors.New("queue item cancelled")
		}

		if status.Executable != nil && status.Executable.Number > 0 {
			return status.Executable.Number, nil
		}

		if time.Now().After(deadline) {
			return 0, errors.New("timed out waiting for run to start")
		}

		time.Sleep(1 * time.Second)
	}
}

func monitorRun(cmd *cobra.Command, client *jenkins.Client, jobPath string, buildNumber int64, interval time.Duration, streamLogs bool) (string, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		logCtx   context.Context
		cancel   context.CancelFunc
		logErrCh chan error
	)
	if streamLogs {
		logCtx, cancel = context.WithCancel(ctx)
		defer cancel()
		logErrCh = make(chan error, 1)
		go func() {
			err := shared.StreamProgressiveLog(logCtx, client, jobPath, int(buildNumber), interval, cmd.OutOrStdout())
			logErrCh <- err
		}()
	}

	statusPath := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	lastStatus := time.Time{}
	for {
		var detail runDetail
		_, err := client.Do(client.NewRequest(), http.MethodGet, statusPath, &detail)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			if logErrCh != nil {
				<-logErrCh
			}
			return "", err
		}

		if !detail.Building {
			if cancel != nil {
				cancel()
			}
			if logErrCh != nil {
				if err := <-logErrCh; err != nil {
					return "", err
				}
			}
			result := strings.ToUpper(detail.Result)
			if result == "" {
				result = "SUCCESS"
			}
			if streamLogs {
				fmt.Fprintf(cmd.OutOrStdout(), "\nRun #%d completed with status %s\n", detail.Number, result)
			}
			return result, nil
		}

		if streamLogs && time.Since(lastStatus) >= 5*time.Second {
			fmt.Fprintf(cmd.OutOrStdout(), "Run #%d still running...\n", detail.Number)
			lastStatus = time.Now()
		}
		time.Sleep(2 * time.Second)
	}
}

func exitCodeForResult(result string) int {
	switch strings.ToUpper(result) {
	case "SUCCESS":
		return 0
	case "UNSTABLE":
		return 10
	case "FAILURE":
		return 11
	case "ABORTED":
		return 12
	case "NOT_BUILT":
		return 13
	default:
		return 0
	}
}
