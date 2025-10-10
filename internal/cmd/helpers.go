package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/config"
	"github.com/your-org/jenkins-cli/internal/jenkins"
	"gopkg.in/yaml.v3"
)

func resolveContextName(cmd *cobra.Command, cfg *config.Config) (string, error) {
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

	_, name, err := cfg.ActiveContext()
	if err != nil && !errors.Is(err, config.ErrContextNotFound) {
		return "", err
	}
	return name, nil
}

func newJenkinsClient(cmd *cobra.Command) (*jenkins.Client, error) {
	cfg := ConfigFromCmd(cmd)
	if cfg == nil {
		return nil, errors.New("configuration unavailable")
	}

	name, err := resolveContextName(cmd, cfg)
	if err != nil {
		return nil, err
	}

	client, err := jenkins.NewClient(context.Background(), cfg, name)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func wantsJSON(cmd *cobra.Command) bool {
	v, _ := cmd.Root().PersistentFlags().GetBool("json")
	return v
}

func wantsYAML(cmd *cobra.Command) bool {
	v, _ := cmd.Root().PersistentFlags().GetBool("yaml")
	return v
}

func printOutput(cmd *cobra.Command, data interface{}, human func() error) error {
	if wantsJSON(cmd) {
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	if wantsYAML(cmd) {
		encoded, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	return human()
}
