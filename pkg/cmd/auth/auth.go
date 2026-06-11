package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
	"github.com/avivsinai/jenkins-cli/internal/terminal"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with Jenkins instances",
	}

	cmd.AddCommand(
		newAuthLoginCmd(f),
		newAuthLogoutCmd(f),
		newAuthStatusCmd(f),
	)

	return cmd
}

type authLoginOptions struct {
	name               string
	username           string
	token              string
	insecure           bool
	proxy              string
	caFile             string
	setActive          bool
	allowInsecureStore bool
	noVerify           bool
}

const (
	usernameFlagHelp = "Jenkins user ID (Google/SSO users: usually your email)"
	usernamePrompt   = "Username (Jenkins user ID, often your email)"
	tokenPrompt      = "Jenkins API token"
)

func newAuthLoginCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &authLoginOptions{setActive: true}

	cmd := &cobra.Command{
		Use:   "login <url>",
		Short: "Authenticate to Jenkins and persist a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
			}
			return runAuthLogin(cmd, cfg, opts, args[0])
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "Context name (defaults to Jenkins hostname)")
	cmd.Flags().StringVar(&opts.username, "username", "", usernameFlagHelp)
	cmd.Flags().StringVar(&opts.token, "token", "", "Jenkins API token")
	cmd.Flags().BoolVar(&opts.insecure, "insecure", false, "Skip TLS certificate verification")
	cmd.Flags().StringVar(&opts.proxy, "proxy", "", "Proxy URL for this context")
	cmd.Flags().StringVar(&opts.caFile, "ca-file", "", "Custom CA bundle for TLS verification")
	cmd.Flags().BoolVar(&opts.setActive, "set-active", true, "Set the context as active after login")
	cmd.Flags().BoolVar(&opts.allowInsecureStore, "allow-insecure-store", false, "Allow encrypted file-based secret storage")
	cmd.Flags().BoolVar(&opts.noVerify, "no-verify", false, "Skip credential verification against the controller")

	return cmd
}

func runAuthLogin(cmd *cobra.Command, cfg *config.Config, opts *authLoginOptions, rawURL string) error {
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
		if username, err = terminal.Prompt(usernamePrompt, ""); err != nil {
			return fmt.Errorf("read username: %w", err)
		}
	}

	token := opts.token
	if token == "" {
		if token, err = terminal.PromptSecret(tokenPrompt); err != nil {
			return fmt.Errorf("read token: %w", err)
		}
	}

	storeOpts := []secret.Option{}
	if opts.allowInsecureStore {
		storeOpts = append(storeOpts, secret.WithAllowFileFallback(true))
	}

	store, err := secret.Open(storeOpts...)
	if err != nil {
		return fmt.Errorf("open secret store: %w", err)
	}

	key := secret.TokenKey(contextName)

	// Snapshot the pre-login state so a failed verification can restore it:
	// a login with bad credentials must not replace a working context, token,
	// or active-context selection.
	var prevCtx *config.Context
	if existing, ctxErr := cfg.Context(contextName); ctxErr == nil && existing != nil {
		snapshot := *existing
		prevCtx = &snapshot
	}
	prevActive := cfg.Active
	prevToken, prevTokenErr := store.Get(key)
	hadToken := prevTokenErr == nil

	cfg.SetContext(contextName, &config.Context{
		URL:                parsed.String(),
		Username:           username,
		Insecure:           opts.insecure,
		Proxy:              opts.proxy,
		CAFile:             opts.caFile,
		AllowInsecureStore: opts.allowInsecureStore,
	})

	if opts.setActive {
		if err := cfg.SetActive(contextName); err != nil {
			return fmt.Errorf("set active context: %w", err)
		}
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// On darwin, an existing Keychain item's ACL is preserved by the update
	// path in 99designs/keyring (kcItem.SetAccess(nil) in updateItem). That
	// means a stale ACL entry from a previous jk binary with a different
	// Designated Requirement — typically after a Homebrew upgrade — keeps
	// prompting forever. Delete first so Set() takes the create path with the
	// current binary's DR as the trusted app.
	if runtime.GOOS == "darwin" {
		if err := store.Delete(key); err != nil {
			return fmt.Errorf("refresh token entry: %w", err)
		}
	}

	if err := store.Set(key, token); err != nil {
		return fmt.Errorf("store token: %w", err)
	}

	if opts.noVerify {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s (%s)\n", parsed.String(), contextName)
		return nil
	}

	who, verifyErr := verifyLogin(cmd, cfg, contextName)
	switch {
	case verifyErr == nil:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s as %s (%s)\n", parsed.String(), who.Name, contextName)
		return nil
	case errors.Is(verifyErr, errVerifyInconclusive):
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v; credentials saved unverified\n", verifyErr)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s (%s)\n", parsed.String(), contextName)
		return nil
	default:
		rollbackLogin(cfg, store, contextName, key, prevCtx, prevActive, prevToken, hadToken)
		return loginVerificationError(verifyErr, parsed, contextName, prevCtx != nil)
	}
}

// errVerifyInconclusive marks verification outcomes where Jenkins could not
// be reached or answered unexpectedly — distinct from a definite credential
// rejection, which rolls the login back.
var errVerifyInconclusive = errors.New("could not verify credentials")

// verifyLogin asks Jenkins how the just-stored credentials authenticate.
// A nil error means the credentials are valid. Errors wrapping
// errVerifyInconclusive mean Jenkins could not be consulted; any other error
// is a definite rejection.
func verifyLogin(cmd *cobra.Command, cfg *config.Config, contextName string) (*jenkins.WhoAmI, error) {
	ctx := cmd.Context()

	client, err := jenkins.NewClient(ctx, cfg, contextName,
		jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errVerifyInconclusive, err)
	}

	who, resp, err := client.WhoAmI(ctx)
	switch {
	case err != nil && errors.Is(err, jenkins.ErrSSORedirect):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("%w: %v", errVerifyInconclusive, err)
	case resp.StatusCode() == http.StatusUnauthorized || resp.StatusCode() == http.StatusForbidden:
		return nil, fmt.Errorf("jenkins rejected the credentials (%s)", resp.Status())
	case resp.IsSuccess() && who.Authenticated:
		return who, nil
	case resp.IsSuccess():
		return nil, errors.New("jenkins treated the request as anonymous")
	default:
		return nil, fmt.Errorf("%w: unexpected response %s", errVerifyInconclusive, resp.Status())
	}
}

// rollbackLogin restores the pre-login context, token, and active-context
// selection after a definite verification failure. Best effort: rollback
// errors are not surfaced over the primary verification error.
func rollbackLogin(cfg *config.Config, store *secret.Store, contextName, key string, prevCtx *config.Context, prevActive string, prevToken string, hadToken bool) {
	if prevCtx != nil {
		cfg.SetContext(contextName, prevCtx)
	} else {
		cfg.RemoveContext(contextName)
	}
	_ = cfg.SetActive(prevActive)
	_ = cfg.Save()

	if hadToken {
		_ = store.Set(key, prevToken)
	} else {
		_ = store.Delete(key)
	}
}

func loginVerificationError(verifyErr error, parsed *url.URL, contextName string, restored bool) error {
	outcome := fmt.Sprintf("context %q was not saved", contextName)
	if restored {
		outcome = fmt.Sprintf("the previous configuration for context %q was restored", contextName)
	}

	// The SSO redirect error already carries full token guidance.
	if errors.Is(verifyErr, jenkins.ErrSSORedirect) {
		return fmt.Errorf("login verification failed: %w; %s", verifyErr, outcome)
	}

	base := strings.TrimSuffix(parsed.String(), "/")
	return fmt.Errorf("login verification failed: %v; %s. "+
		"Use a Jenkins API token (create one at %s/me/configure), not a password or SSO token; "+
		"for Google/SSO realms the Jenkins user ID is usually the full email address. "+
		"Use --no-verify to store credentials without checking them",
		verifyErr, outcome, base)
}

func deriveContextName(u *url.URL) string {
	host := strings.ReplaceAll(u.Hostname(), ".", "-")
	host = strings.ToLower(host)
	if host == "" {
		return "default"
	}
	return host
}

func newAuthLogoutCmd(f *cmdutil.Factory) *cobra.Command {
	var contextName string

	cmd := &cobra.Command{
		Use:   "logout [context]",
		Short: "Remove credentials for a context",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
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

			ctxDef, err := cfg.Context(contextName)
			if err != nil {
				if errors.Is(err, config.ErrContextNotFound) {
					return fmt.Errorf("context %q not found", contextName)
				}
				return err
			}

			storeOpts := []secret.Option{}
			if ctxDef != nil && ctxDef.AllowInsecureStore {
				storeOpts = append(storeOpts, secret.WithAllowFileFallback(true))
			}

			store, err := secret.Open(storeOpts...)
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

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged out of context %s\n", contextName)
			return nil
		},
	}

	cmd.Flags().StringVar(&contextName, "context", "", "Context name to remove (defaults to active)")
	return cmd
}

func newAuthStatusCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := f.ResolveConfig()
			if err != nil {
				return err
			}

			ctx, name, err := cfg.ActiveContext()
			if err != nil && !errors.Is(err, config.ErrContextNotFound) {
				return err
			}

			if ctx == nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No active context")
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Active context: %s\n", name)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", ctx.URL)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", ctx.Username)
			return nil
		},
	}
}
