package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/pkg/config"
	"github.com/your-org/jenkins-cli/pkg/secret"
	"github.com/your-org/jenkins-cli/pkg/terminal"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with Jenkins instances",
	}

	cmd.AddCommand(
		newAuthLoginCmd(),
		newAuthLogoutCmd(),
		newAuthStatusCmd(),
	)

	return cmd
}

type authLoginOptions struct {
	name        string
	username    string
	token       string
	insecure    bool
	proxy       string
	caFile      string
	setActive   bool
	contextFlag string
}

func newAuthLoginCmd() *cobra.Command {
	opts := &authLoginOptions{setActive: true}
	cmd := &cobra.Command{
		Use:   "login <url>",
		Short: "Authenticate to Jenkins and persist a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd, opts, args[0])
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "Context name (defaults to Jenkins hostname)")
	cmd.Flags().StringVar(&opts.username, "username", "", "Jenkins username")
	cmd.Flags().StringVar(&opts.token, "token", "", "Jenkins API token")
	cmd.Flags().BoolVar(&opts.insecure, "insecure", false, "Skip TLS certificate verification")
	cmd.Flags().StringVar(&opts.proxy, "proxy", "", "Proxy URL for this context")
	cmd.Flags().StringVar(&opts.caFile, "ca-file", "", "Custom CA bundle for TLS verification")
	cmd.Flags().BoolVar(&opts.setActive, "set-active", true, "Set the context as active after login")

	return cmd
}

func runAuthLogin(cmd *cobra.Command, opts *authLoginOptions, rawURL string) error {
	cfg := ConfigFromCmd(cmd)
	if cfg == nil {
		return errors.New("configuration unavailable")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid Jenkins URL %q", rawURL)
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	contextName := opts.name
	if contextName == "" {
		contextName = deriveContextName(parsed)
	}

	username := opts.username
	if username == "" {
		if username, err = terminal.Prompt("Username", ""); err != nil {
			return fmt.Errorf("read username: %w", err)
		}
	}

	token := opts.token
	if token == "" {
		if token, err = terminal.PromptSecret("API token"); err != nil {
			return fmt.Errorf("read token: %w", err)
		}
	}

	store, err := secret.Open()
	if err != nil {
		return fmt.Errorf("open secret store: %w", err)
	}

	cfg.SetContext(contextName, &config.Context{
		URL:      parsed.String(),
		Username: username,
		Insecure: opts.insecure,
		Proxy:    opts.proxy,
		CAFile:   opts.caFile,
	})

	if opts.setActive {
		if err := cfg.SetActive(contextName); err != nil {
			return fmt.Errorf("set active context: %w", err)
		}
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if err := store.Set(secret.TokenKey(contextName), token); err != nil {
		return fmt.Errorf("store token: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s (%s)\n", parsed.String(), contextName)
	return nil
}

func deriveContextName(u *url.URL) string {
	host := strings.ReplaceAll(u.Hostname(), ".", "-")
	host = strings.ToLower(host)
	if host == "" {
		return "default"
	}
	return host
}

func newAuthLogoutCmd() *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "logout [context]",
		Short: "Remove credentials for a context",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromCmd(cmd)
			if cfg == nil {
				return errors.New("configuration unavailable")
			}

			if len(args) == 1 {
				contextName = args[0]
			}

			if contextName == "" {
				name := cfg.Active
				if name == "" {
					return errors.New("no context specified and no active context")
				}
				contextName = name
			}

			store, err := secret.Open()
			if err != nil {
				return fmt.Errorf("open secret store: %w", err)
			}

			cfg.RemoveContext(contextName)
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			if err := store.Delete(secret.TokenKey(contextName)); err != nil {
				return fmt.Errorf("delete token: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Logged out of context %s\n", contextName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Context name to remove (defaults to active)")
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := ConfigFromCmd(cmd)
			if cfg == nil {
				return errors.New("configuration unavailable")
			}

			ctx, name, err := cfg.ActiveContext()
			if err != nil {
				return fmt.Errorf("resolve active context: %w", err)
			}

			if ctx == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No active context")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Active context: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", ctx.URL)
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", ctx.Username)
			return nil
		},
	}
}
