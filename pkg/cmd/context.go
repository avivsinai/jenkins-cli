package cmd

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/config"
	"github.com/your-org/jenkins-cli/pkg/secret"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage Jenkins contexts",
	}

	cmd.AddCommand(
		newContextListCmd(),
		newContextUseCmd(),
		newContextRemoveCmd(),
	)

	return cmd
}

func newContextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List configured contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromCmd(cmd)
			if cfg == nil {
				return errors.New("configuration unavailable")
			}

			cfgContexts := cfg.Contexts
			if len(cfgContexts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured")
				return nil
			}

			names := make([]string, 0, len(cfgContexts))
			for name := range cfgContexts {
				names = append(names, name)
			}
			sort.Strings(names)

			active := cfg.Active
			for _, name := range names {
				prefix := " "
				if name == active {
					prefix = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", prefix, name, cfgContexts[name].URL)
			}
			return nil
		},
	}
}

func newContextUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromCmd(cmd)
			if cfg == nil {
				return errors.New("configuration unavailable")
			}

			name := args[0]
			if err := cfg.SetActive(name); err != nil {
				if errors.Is(err, config.ErrContextNotFound) {
					return fmt.Errorf("context %q not found", name)
				}
				return err
			}

			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %s\n", name)
			return nil
		},
	}
}

func newContextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a context and its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromCmd(cmd)
			if cfg == nil {
				return errors.New("configuration unavailable")
			}
			name := args[0]

			if _, err := cfg.Context(name); err != nil {
				if errors.Is(err, config.ErrContextNotFound) {
					return fmt.Errorf("context %q not found", name)
				}
				return err
			}

			store, err := secret.Open()
			if err != nil {
				return fmt.Errorf("open secret store: %w", err)
			}

			cfg.RemoveContext(name)
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			if err := store.Delete(secret.TokenKey(name)); err != nil {
				return fmt.Errorf("delete token: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed context %s\n", name)
			return nil
		},
	}
}
