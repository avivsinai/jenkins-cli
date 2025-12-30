package run

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
)

// printRunSummary outputs a compact, human-readable build summary with optional colors.
func printRunSummary(cmd *cobra.Command, output runDetailOutput) error {
	w := cmd.OutOrStdout()

	// Determine if colors should be used
	useColors := !noColor()

	// Status with color/symbol
	var statusSymbol, statusColor, reset string
	if useColors {
		reset = "\033[0m"
	}

	switch strings.ToUpper(output.Result) {
	case "SUCCESS":
		statusSymbol = "✓"
		if useColors {
			statusColor = "\033[32m" // green
		}
	case "FAILURE":
		statusSymbol = "✗"
		if useColors {
			statusColor = "\033[31m" // red
		}
	case "UNSTABLE":
		statusSymbol = "!"
		if useColors {
			statusColor = "\033[33m" // yellow
		}
	case "ABORTED":
		statusSymbol = "⊘"
		if useColors {
			statusColor = "\033[90m" // gray
		}
	default:
		statusSymbol = "○"
		if useColors {
			statusColor = "\033[36m" // cyan (running/unknown)
		}
	}

	// Format duration
	duration := formatDuration(output.DurationMs)

	// Determine result text (for running builds, show status)
	resultText := output.Result
	if resultText == "" {
		resultText = strings.ToUpper(output.Status)
	}

	// Print summary
	fmt.Fprintf(w, "Build #%d %s%s %s%s\n", output.Number, statusColor, resultText, statusSymbol, reset)
	fmt.Fprintf(w, "Duration: %s\n", duration)
	if output.StartTime != "" {
		fmt.Fprintf(w, "Started:  %s\n", output.StartTime)
	}
	if output.URL != "" {
		fmt.Fprintf(w, "URL:      %s\n", output.URL)
	}

	// Test results if available
	if output.Tests != nil && output.Tests.Total > 0 {
		passed := output.Tests.Total - output.Tests.Failed - output.Tests.Skipped
		fmt.Fprintf(w, "Tests:    %d total, %d passed, %d failed, %d skipped\n",
			output.Tests.Total,
			passed,
			output.Tests.Failed,
			output.Tests.Skipped)
	}

	return nil
}

// formatDuration formats milliseconds into a human-readable duration string.
func formatDuration(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	d := time.Duration(ms) * time.Millisecond

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		if secs > 0 {
			return fmt.Sprintf("%dm %ds", mins, secs)
		}
		return fmt.Sprintf("%dm", mins)
	}

	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if mins > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dh", hours)
}

// noColor returns true if color output should be disabled.
// Respects NO_COLOR environment variable per https://no-color.org/
func noColor() bool {
	_, noColorSet := os.LookupEnv("NO_COLOR")
	return noColorSet
}

// formatRunListSummary formats a list of runs as a compact summary.
// This can be used by run ls --summary in the future.
func formatRunListSummary(w interface{ Write([]byte) (int, error) }, items []runListItem) error {
	useColors := !noColor()

	var reset, green, red, yellow, gray, cyan string
	if useColors {
		reset = "\033[0m"
		green = "\033[32m"
		red = "\033[31m"
		yellow = "\033[33m"
		gray = "\033[90m"
		cyan = "\033[36m"
	}

	for _, item := range items {
		var statusColor, statusSymbol string
		switch strings.ToUpper(item.Result) {
		case "SUCCESS":
			statusColor = green
			statusSymbol = "✓"
		case "FAILURE":
			statusColor = red
			statusSymbol = "✗"
		case "UNSTABLE":
			statusColor = yellow
			statusSymbol = "!"
		case "ABORTED":
			statusColor = gray
			statusSymbol = "⊘"
		default:
			statusColor = cyan
			statusSymbol = "○"
		}

		result := item.Result
		if result == "" {
			result = strings.ToUpper(item.Status)
		}

		duration := shared.DurationString(item.DurationMs)
		fmt.Fprintf(w, "#%d\t%s%s %s%s\t%s\t%s\n",
			item.Number,
			statusColor, result, statusSymbol, reset,
			item.StartTime,
			duration)
	}

	return nil
}
