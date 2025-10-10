package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/jenkins"
)

type runListResponse struct {
	Builds []runSummary `json:"builds"`
}

type runSummary struct {
	Number    int64  `json:"number"`
	Result    string `json:"result"`
	Building  bool   `json:"building"`
	Timestamp int64  `json:"timestamp"`
	Duration  int64  `json:"duration"`
}

type runDetail struct {
	Number     int64            `json:"number"`
	Result     string           `json:"result"`
	Building   bool             `json:"building"`
	Timestamp  int64            `json:"timestamp"`
	Duration   int64            `json:"duration"`
	URL        string           `json:"url"`
	Actions    []map[string]any `json:"actions"`
	Parameters []map[string]any `json:"parameters"`
	Stages     []map[string]any `json:"stages"`
	ChangeSet  map[string]any   `json:"changeSet"`
}

type queueItemStatus struct {
	ID         int64            `json:"id"`
	Why        string           `json:"why"`
	Cancelled  bool             `json:"cancelled"`
	Executable *queueExecutable `json:"executable"`
}

type queueExecutable struct {
	Number int64 `json:"number"`
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Interact with job runs",
	}

	cmd.AddCommand(
		newRunStartCmd(),
		newRunListCmd(),
		newRunViewCmd(),
	)

	return cmd
}

func newRunStartCmd() *cobra.Command {
	var params []string
	var follow bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "start <jobPath>",
		Short: "Trigger a job run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			encoded := jenkins.EncodeJobPath(args[0])
			if encoded == "" {
				return errors.New("job path is required")
			}

			methodPath := fmt.Sprintf("/%s/build", encoded)
			req := client.NewRequest()
			if len(params) > 0 {
				data := make(map[string]string)
				for _, p := range params {
					parts := strings.SplitN(p, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid parameter %q", p)
					}
					data[parts[0]] = parts[1]
				}
				req.SetFormData(data)
				methodPath = fmt.Sprintf("/%s/buildWithParameters", encoded)
			}

			resp, err := client.Do(req, http.MethodPost, methodPath, nil)
			if err != nil {
				return err
			}

			if resp.StatusCode() >= 300 {
				return fmt.Errorf("trigger build failed: %s", resp.Status())
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Triggered run for %s\n", args[0])

			if !follow {
				return nil
			}

			queueLocation := resp.Header().Get("Location")
			if queueLocation == "" {
				queueLocation = resp.Header().Get("X-Queue-Item")
			}

			buildNumber, err := waitForBuildNumber(client, queueLocation, 5*time.Minute)
			if err != nil {
				return err
			}

			result, err := monitorRun(cmd, client, args[0], buildNumber, interval)
			if err != nil {
				return err
			}

			code := exitCodeForResult(result)
			if code == 0 {
				return nil
			}
			return newExitError(code, "")
		},
	}

	cmd.Flags().StringSliceVarP(&params, "param", "p", nil, "Build parameter key=value")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the run progress until completion")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval when following runs")
	return cmd
}

func newRunListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "ls <jobPath>",
		Short: "List recent runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			encoded := jenkins.EncodeJobPath(args[0])
			path := fmt.Sprintf("/%s/api/json", encoded)

			query := "builds[number,result,building,timestamp,duration]"
			if limit > 0 {
				query = fmt.Sprintf("builds[number,result,building,timestamp,duration]{,%d}", limit)
			}

			var resp runListResponse
			_, err = client.Do(client.NewRequest().SetQueryParam("tree", query), http.MethodGet, path, &resp)
			if err != nil {
				return err
			}

			return printOutput(cmd, resp.Builds, func() error {
				if len(resp.Builds) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No runs found")
					return nil
				}
				for _, build := range resp.Builds {
					status := build.Result
					if build.Building {
						status = "BUILDING"
					}
					t := time.UnixMilli(build.Timestamp)
					fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\t%s\t%s\n", build.Number, status, t.UTC().Format(time.RFC3339), durationString(build.Duration))
				}
				return nil
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Number of runs to list")
	return cmd
}

func newRunViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <jobPath> <buildNumber>",
		Short: "View run details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
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

			return printOutput(cmd, detail, func() error {
				status := detail.Result
				if detail.Building {
					status = "BUILDING"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Run #%d\nStatus: %s\nURL: %s\nStarted: %s\nDuration: %s\n", detail.Number, status, detail.URL, time.UnixMilli(detail.Timestamp).UTC().Format(time.RFC3339), durationString(detail.Duration))
				return nil
			})
		},
	}

	return cmd
}

func durationString(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	d := time.Duration(ms) * time.Millisecond
	return d.String()
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

func monitorRun(cmd *cobra.Command, client *jenkins.Client, jobPath string, buildNumber int64, interval time.Duration) (string, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	logErrCh := make(chan error, 1)
	go func() {
		err := streamProgressiveLog(logCtx, client, jobPath, int(buildNumber), interval, cmd.OutOrStdout())
		logErrCh <- err
	}()

	statusPath := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	lastStatus := time.Time{}
	for {
		var detail runDetail
		_, err := client.Do(client.NewRequest(), http.MethodGet, statusPath, &detail)
		if err != nil {
			cancel()
			<-logErrCh
			return "", err
		}

		if !detail.Building {
			cancel()
			if err := <-logErrCh; err != nil {
				return "", err
			}
			result := strings.ToUpper(detail.Result)
			if result == "" {
				result = "SUCCESS"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nRun #%d completed with status %s\n", detail.Number, result)
			return result, nil
		}

		if time.Since(lastStatus) >= 5*time.Second {
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
