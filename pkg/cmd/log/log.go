package logcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type logOptions struct {
	jobPath     string
	buildString string
	follow      bool
	interval    time.Duration
	plain       bool
	maxBytes    int
}

type logOutput struct {
	JobPath   string `json:"jobPath"`
	Build     int64  `json:"build"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	StartTime string `json:"startTime,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Log       string `json:"log"`
	Truncated bool   `json:"truncated,omitempty"`
}

type runDetail struct {
	Building          bool   `json:"building"`
	Result            string `json:"result"`
	Timestamp         int64  `json:"timestamp"`
	Duration          int64  `json:"duration"`
	EstimatedDuration int64  `json:"estimatedDuration"`
}

func NewCmdLog(f *cmdutil.Factory) *cobra.Command {
	opts := &logOptions{
		interval: time.Second,
		maxBytes: 2 * 1024 * 1024, // 2 MiB snapshot when not following
	}

	cmd := &cobra.Command{
		Use:   "log <jobPath> <buildNumber>",
		Short: "Show Jenkins run logs",
		Long:  "Display the console log for a Jenkins run. Add --follow to stream live output similar to `gh run view --log`.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.jobPath = args[0]
			opts.buildString = args[1]
			return runLog(cmd, f, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.follow, "follow", false, "Stream log output until the run finishes")
	cmd.Flags().DurationVar(&opts.interval, "interval", time.Second, "Polling interval while following live logs")
	cmd.Flags().BoolVar(&opts.plain, "plain", false, "Disable headings and additional formatting")
	return cmd
}

func runLog(cmd *cobra.Command, f *cmdutil.Factory, opts *logOptions) error {
	client, err := shared.JenkinsClient(cmd, f)
	if err != nil {
		return err
	}

	num, err := strconv.ParseInt(opts.buildString, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid build number: %w", err)
	}
	if num <= 0 {
		return errors.New("build number must be positive")
	}

	encoded := jenkins.EncodeJobPath(opts.jobPath)
	if encoded == "" {
		return errors.New("job path is required")
	}

	path := fmt.Sprintf("/%s/%d/api/json", encoded, num)
	detail := &runDetail{}
	resp, err := client.Do(client.NewRequest(), http.MethodGet, path, detail)
	if err != nil {
		return err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return shared.NewExitError(3, fmt.Sprintf("run %s #%d not found", opts.jobPath, num))
	}

	status := statusFromFlags(detail.Building)
	result := strings.ToUpper(strings.TrimSpace(detail.Result))
	if status == "completed" && result == "" {
		result = "SUCCESS"
	}

	if opts.follow {
		if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
			return errors.New("--json/--yaml not supported with --follow")
		}
		return streamLogFollow(cmd, client, opts, int(num), detail, status, result)
	}

	return renderLogSnapshot(cmd, client, opts, int(num), detail, status, result)
}

func streamLogFollow(cmd *cobra.Command, client *jenkins.Client, opts *logOptions, buildNumber int, detail *runDetail, status, result string) error {
	if !opts.plain && !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd) {
		printLogHeading(cmd.OutOrStdout(), opts.jobPath, int64(buildNumber), detail, status, result)
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := shared.StreamProgressiveLog(ctx, client, opts.jobPath, buildNumber, opts.interval, cmd.OutOrStdout()); err != nil {
		return err
	}

	if !opts.plain {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run status: %s", strings.ToUpper(result))
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func renderLogSnapshot(cmd *cobra.Command, client *jenkins.Client, opts *logOptions, buildNumber int, detail *runDetail, status, result string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var buf bytes.Buffer
	truncated, err := shared.CollectLogSnapshot(ctx, client, opts.jobPath, buildNumber, opts.maxBytes, &buf)
	if err != nil {
		return err
	}

	output := logOutput{
		JobPath:   opts.jobPath,
		Build:     int64(buildNumber),
		Status:    status,
		Result:    result,
		Log:       buf.String(),
		Truncated: truncated,
	}
	if detail.Timestamp > 0 {
		output.StartTime = time.UnixMilli(detail.Timestamp).UTC().Format(time.RFC3339)
	}
	if detail.Duration > 0 {
		output.Duration = shared.DurationString(detail.Duration)
	}

	return shared.PrintOutput(cmd, output, func() error {
		writer := cmd.OutOrStdout()
		if !opts.plain {
			printLogHeading(writer, opts.jobPath, int64(buildNumber), detail, status, result)
			_, _ = fmt.Fprintln(writer)
		}

		if buf.Len() == 0 {
			if !opts.plain {
				_, _ = fmt.Fprintln(writer, "(log is empty)")
			}
		} else {
			if _, err := io.Copy(writer, bytes.NewReader(buf.Bytes())); err != nil {
				return err
			}
			if !strings.HasSuffix(buf.String(), "\n") {
				_, _ = fmt.Fprintln(writer)
			}
		}

		if truncated && !opts.plain {
			_, _ = fmt.Fprintln(writer)
			_, _ = fmt.Fprintln(writer, "(log truncated; use --follow to stream live output)")
		}
		return nil
	})
}

func statusFromFlags(building bool) string {
	if building {
		return "running"
	}
	return "completed"
}

func printLogHeading(w io.Writer, jobPath string, buildNumber int64, detail *runDetail, status, result string) {
	_, _ = fmt.Fprintf(w, "==> %s #%d\n", jobPath, buildNumber)
	var pieces []string
	if status != "" {
		pieces = append(pieces, fmt.Sprintf("status: %s", strings.ToUpper(status)))
	}
	if result != "" {
		pieces = append(pieces, fmt.Sprintf("result: %s", strings.ToUpper(result)))
	}
	if detail != nil {
		if detail.Timestamp > 0 {
			pieces = append(pieces, fmt.Sprintf("started: %s", time.UnixMilli(detail.Timestamp).UTC().Format(time.RFC3339)))
		}
		if detail.Duration > 0 {
			pieces = append(pieces, fmt.Sprintf("duration: %s", shared.DurationString(detail.Duration)))
		}
	}
	if len(pieces) > 0 {
		_, _ = fmt.Fprintf(w, "   %s\n", strings.Join(pieces, "   "))
	}
}
