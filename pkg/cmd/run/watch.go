package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/filter"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type runWatchOptions struct {
	commit     string
	interval   time.Duration
	timeout    time.Duration
	exitStatus bool
}

type runWatchOutput struct {
	JobPath    string `json:"jobPath"`
	Commit     string `json:"commit"`
	Number     int64  `json:"number"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	Result     string `json:"result"`
	DurationMs int64  `json:"durationMs"`
}

func newRunWatchCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &runWatchOptions{}
	cmd := &cobra.Command{
		Use:   "watch <jobPath> --commit <sha>",
		Short: "Wait for a commit's build, then follow it to completion",
		Long:  "Watch the newest retained build of a full source commit SHA. Wait for a build if none exists yet, then follow that build number to completion. Queue entries before a matching build exists are job-level context and are not attributed to the commit.",
		Example: `  jk run watch team/app/main --commit 0123456789012345678901234567890123456789 --exit-status
  jk run watch team/app/main --commit 0123456789012345678901234567890123456789 --timeout 20m --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.commit = strings.ToLower(strings.TrimSpace(opts.commit))
			if !watchCommitPattern.MatchString(opts.commit) {
				return errors.New("--commit must be a full 40- or 64-character hexadecimal source commit SHA")
			}
			if opts.interval <= 0 {
				return errors.New("--interval must be positive")
			}
			if opts.timeout < 0 {
				return errors.New("--timeout must not be negative")
			}
			if err := shared.ValidateOutputFlags(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if opts.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, opts.timeout)
				defer cancel()
			}
			cmd.SetContext(ctx)
			output := runWatchOutput{JobPath: normalizeJobPath(args[0]), Commit: opts.commit, Status: "waiting"}
			client, err := shared.JenkinsClient(cmd, f)
			if err == nil {
				err = watchCommitRun(ctx, client, opts, &output, cmd.ErrOrStderr())
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				output.Status = "timed_out"
			} else if errors.Is(err, context.Canceled) {
				output.Status = "cancelled"
			} else if err != nil {
				return err
			}
			if printErr := shared.PrintOutput(cmd, output, func() error { return nil }); printErr != nil {
				return printErr
			}
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				return shared.NewExitError(124, "timeout waiting for the commit's build")
			case errors.Is(err, context.Canceled):
				return shared.NewExitError(130, "commit watch cancelled")
			case opts.exitStatus:
				code := watchExitCode(output.Result)
				if code != 0 {
					return shared.NewExitError(code, "")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.commit, "commit", "", "Full source commit SHA to watch (required)")
	cmd.Flags().DurationVar(&opts.interval, "interval", 2*time.Second, "Polling interval")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 0, "Maximum total discovery and execution wait (0 = no timeout)")
	cmd.Flags().BoolVar(&opts.exitStatus, "exit-status", false, "Exit with the final build result code")
	return cmd
}

var watchCommitPattern = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)
var watchETAPattern = regexp.MustCompile(`(?i)\s*\(ETA:[^)]*\)`)

// watchCommitRun keeps progress separate from the one final structured result.
// Every network request carries ctx, including discovery and queue requests.
func watchCommitRun(ctx context.Context, client *jenkins.Client, opts *runWatchOptions, output *runWatchOutput, progress io.Writer) error {
	var lastState string
	emit := func(state string) {
		if state != lastState {
			_, _ = fmt.Fprintln(progress, state)
			lastState = state
		}
	}
	emit(fmt.Sprintf("Waiting for a build of %s in %s", opts.commit, output.JobPath))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if output.Number == 0 {
			match, err := findCommitRun(ctx, client, output.JobPath, opts.commit)
			if err != nil {
				return err
			}
			if match != nil {
				output.Number = match.Number
				output.URL = match.URL
			}
		}
		if output.Number > 0 {
			var detail runDetail
			path := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(output.JobPath), output.Number)
			req := client.NewRequest().SetContext(ctx).SetQueryParam("tree", "number,url,building,result,duration")
			if _, err := client.Do(req, http.MethodGet, path, &detail); err != nil {
				return err
			}
			if detail.URL != "" {
				output.URL = detail.URL
			}
			output.DurationMs = detail.Duration
			output.Result = strings.ToUpper(strings.TrimSpace(detail.Result))
			if !detail.Building {
				if output.Result == "" {
					output.Result = "UNKNOWN"
				}
				output.Status = "completed"
				emit(fmt.Sprintf("Finished #%d: %s (%s)", output.Number, output.Result, shared.DurationString(detail.Duration)))
				return nil
			}
			output.Status = "running"
		}

		queue, err := watchQueue(ctx, client)
		if err != nil {
			return err
		}
		if output.Number == 0 {
			reasons := watchQueueReasons(queue, func(item queueListItem) bool {
				return watchTaskIsJob(item.Task.URL, output.JobPath)
			})
			if len(reasons) > 0 {
				emit("Job queued (commit not yet identified): " + strings.Join(reasons, "; "))
			} else {
				emit(fmt.Sprintf("Waiting for a build of %s in %s", opts.commit, output.JobPath))
			}
		} else {
			reasons := watchQueueReasons(queue, func(item queueListItem) bool {
				return watchTaskIsBuild(item.Task.URL, output.JobPath, output.Number)
			})
			if len(reasons) > 0 {
				emit(fmt.Sprintf("Waiting for an executor for #%d: %s", output.Number, strings.Join(reasons, "; ")))
			} else {
				emit(fmt.Sprintf("Running #%d: %s", output.Number, output.URL))
			}
		}
		timer := time.NewTimer(opts.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// findCommitRun scans retained history in newest-first pages rather than hiding
// older matching builds behind the run-list command's default limit.
func findCommitRun(ctx context.Context, client *jenkins.Client, jobPath, commit string) (*runListItem, error) {
	const pageSize = 100
	opts := runListOptions{Limit: 1, Filters: []filter.Filter{{Key: "commit", Operator: filter.OpEQ, Value: commit}}}
	var previousLast int64
	for offset := 0; ; offset += pageSize {
		query := buildRunListTree(pageSize, false, false, false)
		query = strings.TrimSuffix(query, fmt.Sprintf("{,%d}", pageSize)) + fmt.Sprintf("{%d,%d}", offset, offset+pageSize)
		// SCM API revisions are needed even when this command is compiled with
		// an older buildRunListTree implementation.
		if !strings.Contains(query, "revision[hash,pullHash]") {
			query = strings.Replace(query, "actions[", "actions[revision[hash,pullHash],", 1)
		}
		req := client.NewRequest().SetContext(ctx).SetQueryParam("tree", query)
		var response runListResponse
		if _, err := client.Do(req, http.MethodGet, "/"+jenkins.EncodeJobPath(jobPath)+"/api/json", &response); err != nil {
			return nil, err
		}
		out, _, err := processRunList(jobPath, opts, response.Builds, false, false, false)
		if err != nil {
			return nil, err
		}
		if len(out.Items) > 0 {
			return &out.Items[0], nil
		}
		if len(response.Builds) < pageSize {
			return nil, nil
		}
		last := response.Builds[len(response.Builds)-1].Number
		if offset > 0 && last >= previousLast {
			return nil, errors.New("Jenkins build history did not advance while paging")
		}
		previousLast = last
	}
}

func watchQueue(ctx context.Context, client *jenkins.Client) ([]queueListItem, error) {
	var queue queueListResponse
	req := client.NewRequest().SetContext(ctx).SetQueryParam("tree", "items[id,why,task[name,url]]")
	_, err := client.Do(req, http.MethodGet, "/queue/api/json", &queue)
	return queue.Items, err
}

func watchQueueReasons(queue []queueListItem, match func(queueListItem) bool) []string {
	sort.Slice(queue, func(i, j int) bool { return queue[i].ID < queue[j].ID })
	var reasons []string
	for _, item := range queue {
		if match(item) {
			reason := strings.TrimSpace(watchETAPattern.ReplaceAllString(item.Why, ""))
			if reason == "" {
				reason = "waiting"
			}
			reasons = append(reasons, fmt.Sprintf("queue #%d: %s", item.ID, reason))
		}
	}
	return reasons
}

// watchTaskPath strips only the Jenkins context prefix. A suffix comparison
// would incorrectly treat folder/app as the top-level app job.
func watchTaskPath(taskURL string) string {
	parsed, err := url.Parse(taskURL)
	if err != nil {
		return ""
	}
	path := "/" + strings.Trim(parsed.EscapedPath(), "/")
	index := strings.Index(path, "/job/")
	if index < 0 {
		return ""
	}
	return strings.TrimPrefix(path[index:], "/")
}

func watchTaskIsJob(taskURL, jobPath string) bool {
	return watchTaskPath(taskURL) == jenkins.EncodeJobPath(jobPath)
}

func watchTaskIsBuild(taskURL, jobPath string, number int64) bool {
	// PlaceholderTask.getUrl exports its owner's run-relative URL. Compare
	// complete job segments plus build number, not the localized task name.
	expected := jenkins.EncodeJobPath(jobPath) + fmt.Sprintf("/%d", number)
	return watchTaskPath(taskURL) == expected
}

func watchExitCode(result string) int {
	switch strings.ToUpper(result) {
	case "SUCCESS", "UNSTABLE", "FAILURE", "ABORTED", "NOT_BUILT", "RUNNING":
		return exitCodeForResult(result)
	default:
		return 1
	}
}
