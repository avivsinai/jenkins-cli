package cmd

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Work with Jenkins run logs",
	}

	cmd.AddCommand(newLogFollowCmd())
	return cmd
}

func newLogFollowCmd() *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "follow <jobPath> <buildNumber>",
		Short: "Stream a build log",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			num, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			return streamProgressiveLog(ctx, client, args[0], num, interval, cmd.OutOrStdout())
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval for new log data")
	return cmd
}
