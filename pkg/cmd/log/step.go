package logcmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
)

func validateStepLogOptions(cmd *cobra.Command, opts *logOptions) error {
	if !cmd.Flags().Changed("step") {
		return nil
	}
	if strings.TrimSpace(opts.stepID) == "" {
		return errors.New("--step requires a node ID from jk run steps")
	}
	if opts.follow {
		return errors.New("--step cannot be combined with --follow")
	}
	for _, name := range []string{"tail", "grep"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
			return fmt.Errorf("--step cannot be combined with --%s", name)
		}
	}
	return nil
}

func renderStepLog(cmd *cobra.Command, client *jenkins.Client, opts *logOptions, build int64, detail *runDetail, status, result string) error {
	ctx := cmd.Context()
	pipeline, err := shared.FetchPipelineSteps(ctx, client, opts.jobPath, build)
	if err != nil {
		return err
	}
	if !pipeline.HasNode(opts.stepID) {
		return fmt.Errorf("Pipeline node %q not found; use jk run steps %s %d", opts.stepID, opts.jobPath, build)
	}
	path := fmt.Sprintf("/%s/%d/stages/log", jenkins.EncodeJobPath(opts.jobPath), build)
	req := client.NewRequest().SetContext(ctx).SetHeader("Accept", "text/plain").
		SetQueryParam("nodeId", opts.stepID).SetDoNotParseResponse(true)
	resp, err := client.DoRaw(req, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	body := resp.RawBody()
	if body == nil {
		return errors.New("step log returned an empty response")
	}
	defer func() { _ = body.Close() }()
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("fetch Pipeline node %s log: %s", opts.stepID, resp.Status())
	}
	limit := opts.maxBytes
	if limit <= 0 {
		limit = 2 * 1024 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return fmt.Errorf("read Pipeline node log: %w", err)
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	output := logOutput{
		JobPath: opts.jobPath, Build: build, Status: status, Result: result,
		Log: string(data), Truncated: truncated, StepID: opts.stepID,
		Complete: !detail.Building && pipeline.Complete && !truncated,
	}
	if detail.Timestamp > 0 {
		output.StartTime = time.UnixMilli(detail.Timestamp).UTC().Format(time.RFC3339)
	}
	if detail.Duration > 0 {
		output.Duration = shared.DurationString(detail.Duration)
	}
	return shared.PrintOutput(cmd, output, func() error {
		out := cmd.OutOrStdout()
		if !opts.plain {
			printLogHeading(out, opts.jobPath, build, detail, status, result)
			if _, err := fmt.Fprintf(out, "Pipeline node: %s\n\n", opts.stepID); err != nil {
				return err
			}
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if truncated && !opts.plain {
			_, err := fmt.Fprintln(out, "(step log truncated at 2 MiB)")
			return err
		}
		return nil
	})
}
