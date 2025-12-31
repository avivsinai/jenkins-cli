package shared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func ResolveContextName(cmd *cobra.Command, cfg *config.Config) (string, error) {
	if cmd == nil {
		return "", errors.New("command is nil")
	}

	if cmd.Flags().Changed("context") {
		name, err := cmd.Flags().GetString("context")
		if err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
		if name != "" {
			return name, nil
		}
	}

	if value, ok := os.LookupEnv("JK_CONTEXT"); ok {
		name := strings.TrimSpace(value)
		if name != "" {
			return name, nil
		}
	}

	_, name, err := cfg.ActiveContext()
	if err != nil && !errors.Is(err, config.ErrContextNotFound) {
		return "", err
	}
	return name, nil
}

// GetOutputFormat returns the requested output format from --format flag.
// Returns empty string for default human-readable output.
// Note: Using --format instead of --output to avoid conflict with artifact --output/-o flag.
func GetOutputFormat(cmd *cobra.Command) string {
	v, _ := cmd.Root().PersistentFlags().GetString("format")
	return strings.ToLower(strings.TrimSpace(v))
}

func WantsJSON(cmd *cobra.Command) bool {
	if v, _ := cmd.Root().PersistentFlags().GetBool("json"); v {
		return true
	}
	return GetOutputFormat(cmd) == "json"
}

func WantsYAML(cmd *cobra.Command) bool {
	if v, _ := cmd.Root().PersistentFlags().GetBool("yaml"); v {
		return true
	}
	return GetOutputFormat(cmd) == "yaml"
}

// WantsTable returns true if table output is requested via --output=table
func WantsTable(cmd *cobra.Command) bool {
	return GetOutputFormat(cmd) == "table"
}

// WantsQuiet returns true if --quiet/-q flag is set or JK_QUIET env var is present.
// Currently supported by: run start, run rerun.
// Other commands (view, cancel, ls) do not implement quiet mode as they primarily
// output structured data where --json is more appropriate.
func WantsQuiet(cmd *cobra.Command) bool {
	if v, _ := cmd.Root().PersistentFlags().GetBool("quiet"); v {
		return true
	}
	_, hasEnv := os.LookupEnv("JK_QUIET")
	return hasEnv
}

func PrintOutput(cmd *cobra.Command, data interface{}, human func() error) error {
	// Validate --jq requires --json.
	if WantsJQ(cmd) && !WantsJSON(cmd) {
		return fmt.Errorf("--jq requires --json flag")
	}

	// Check for conflicting output flags: --json/--yaml boolean flags cannot be used with --format
	jsonFlagSet, _ := cmd.Root().PersistentFlags().GetBool("json")
	yamlFlagSet, _ := cmd.Root().PersistentFlags().GetBool("yaml")
	if (jsonFlagSet || yamlFlagSet) && GetOutputFormat(cmd) != "" {
		return fmt.Errorf("cannot use --json or --yaml with --format flag")
	}

	// Validate --template requires --json or --format=json
	if WantsTemplate(cmd) && !WantsJSON(cmd) {
		return fmt.Errorf("--template requires --json or --format=json")
	}

	if WantsJSON(cmd) {
		// Handle --jq flag
		if WantsJQ(cmd) {
			return ApplyJQ(data, GetJQExpression(cmd), cmd.OutOrStdout())
		}
		// Handle --template flag
		if WantsTemplate(cmd) {
			return ApplyTemplate(data, GetTemplate(cmd), cmd.OutOrStdout())
		}
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	if WantsYAML(cmd) {
		encoded, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	return human()
}

func JenkinsClient(cmd *cobra.Command, f *cmdutil.Factory) (*jenkins.Client, error) {
	cfg, err := f.ResolveConfig()
	if err != nil {
		return nil, err
	}

	name, err := ResolveContextName(cmd, cfg)
	if err != nil {
		return nil, err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	return f.Client(ctx, name)
}
