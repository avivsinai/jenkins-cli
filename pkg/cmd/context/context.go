package contextcmd

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func NewCmdContext(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage Jenkins contexts",
	}

	cmd.AddCommand(
		newContextListCmd(f),
		newContextUseCmd(f),
		newContextRemoveCmd(f),
	)

	return cmd
}

func newContextListCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List configured contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
			}

			cfgContexts := cfg.Contexts
			if len(cfgContexts) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No contexts configured")
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
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", prefix, name, cfgContexts[name].URL)
			}
			return nil
		},
	}
}

func newContextUseCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %s\n", name)
			return nil
		},
	}
}

func newContextRemoveCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a context and its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed context %s\n", name)
			return nil
		},
	}
}
