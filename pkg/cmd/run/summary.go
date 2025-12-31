package run

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
	_, _ = fmt.Fprintf(w, "Build #%d %s%s %s%s\n", output.Number, statusColor, resultText, statusSymbol, reset)
	_, _ = fmt.Fprintf(w, "Duration: %s\n", duration)
	if output.StartTime != "" {
		_, _ = fmt.Fprintf(w, "Started:  %s\n", output.StartTime)
	}
	if output.URL != "" {
		_, _ = fmt.Fprintf(w, "URL:      %s\n", output.URL)
	}

	// Test results if available
	if output.Tests != nil && output.Tests.Total > 0 {
		passed := output.Tests.Total - output.Tests.Failed - output.Tests.Skipped
		_, _ = fmt.Fprintf(w, "Tests:    %d total, %d passed, %d failed, %d skipped\n",
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
// Also disables colors when stdout is not a terminal (piped output).
func noColor() bool {
	_, noColorSet := os.LookupEnv("NO_COLOR")
	if noColorSet {
		return true
	}
	// Also disable colors if not a terminal (piped output)
	fileInfo, _ := os.Stdout.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) == 0
}
