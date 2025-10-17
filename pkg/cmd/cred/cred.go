package cred

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

type credentialItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Scope       string `json:"scope"`
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type credentialsList struct {
	Items      []credentialItem `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type jkCredentialList struct {
	Items      []credentialItem `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

type coreCredentialsResponse struct {
	Credentials []struct {
		ID          string `json:"id"`
		TypeName    string `json:"typeName"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	} `json:"credentials"`
}

var errJKAPINotFound = errors.New("jk credentials endpoint not found")

func NewCmdCred(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cred",
		Short: "Manage Jenkins credentials",
	}

	cmd.AddCommand(
		newCredListCmd(f),
		newCredCreateSecretCmd(f),
		newCredDeleteCmd(f),
	)
	return cmd
}

func newCredListCmd(f *cmdutil.Factory) *cobra.Command {
	var scope string
	var folder string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			scopeVal := strings.ToLower(strings.TrimSpace(scope))
			if scopeVal == "" {
				scopeVal = "system"
			}
			if scopeVal != "system" && scopeVal != "folder" {
				return fmt.Errorf("unsupported scope %q", scope)
			}

			data, err := fetchCredentials(client, scopeVal, folder)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, data, func() error {
				if len(data.Items) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No credentials found")
					return nil
				}
				for _, item := range data.Items {
					if item.Path != "" {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.ID, item.Type, item.Path)
					} else {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", item.ID, item.Type)
					}
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "system", "Scope to query: system or folder")
	cmd.Flags().StringVar(&folder, "folder", "", "Folder path when scope=folder (e.g. team/service)")

	return cmd
}

func fetchCredentials(client *jenkins.Client, scope, folder string) (*credentialsList, error) {
	if scope == "folder" && strings.TrimSpace(folder) == "" {
		return nil, errors.New("folder path required when scope=folder")
	}

	list, err := fetchFromJKAPI(client, scope, folder)
	if err == nil {
		return list, nil
	}
	if !errors.Is(err, errJKAPINotFound) {
		return nil, err
	}

	return fetchFromCoreAPI(client, scope, folder)
}

func fetchFromJKAPI(client *jenkins.Client, scope, folder string) (*credentialsList, error) {
	req := client.NewRequest().SetQueryParam("scope", scope)
	if scope == "folder" {
		req.SetQueryParam("folderPath", folder)
	}

	var resp jkCredentialList
	httpResp, err := client.Do(req, http.MethodGet, "/jk/api/credentials", &resp)
	if err != nil {
		return nil, err
	}

	switch httpResp.StatusCode() {
	case http.StatusOK:
		return &credentialsList{Items: resp.Items, NextCursor: resp.NextCursor}, nil
	case http.StatusNotFound:
		return nil, errJKAPINotFound
	default:
		return nil, fmt.Errorf("jk credentials endpoint: %s", httpResp.Status())
	}
}

func fetchFromCoreAPI(client *jenkins.Client, scope, folder string) (*credentialsList, error) {
	targetPath := "/credentials/store/system/domain/_/api/json"
	displayPath := "system"
	if scope == "folder" {
		encoded := jenkins.EncodeJobPath(folder)
		if encoded == "" {
			return nil, errors.New("invalid folder path")
		}
		targetPath = fmt.Sprintf("/%s/credentials/store/folder/domain/_/api/json", encoded)
		displayPath = folder
	}

	var core coreCredentialsResponse
	resp, err := client.Do(client.NewRequest().SetQueryParam("tree", "credentials[id,typeName,displayName,description]"), http.MethodGet, targetPath, &core)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("credentials endpoint: %s", resp.Status())
	}

	out := &credentialsList{Items: make([]credentialItem, 0, len(core.Credentials))}
	for _, c := range core.Credentials {
		out.Items = append(out.Items, credentialItem{
			ID:          c.ID,
			Type:        c.TypeName,
			Scope:       scope,
			Path:        displayPath,
			Description: firstNonEmpty(c.Description, c.DisplayName),
		})
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func newCredCreateSecretCmd(f *cmdutil.Factory) *cobra.Command {
	var scope string
	var folder string
	var id string
	var description string
	var secret string
	var fromStdin bool

	cmd := &cobra.Command{
		Use:   "create-secret",
		Short: "Create a secret text credential",
		RunE: func(cmd *cobra.Command, args []string) error {
			scopeVal := strings.ToLower(strings.TrimSpace(scope))
			if scopeVal == "" {
				scopeVal = "system"
			}
			if scopeVal != "system" && scopeVal != "folder" {
				return fmt.Errorf("unsupported scope %q", scope)
			}

			if strings.TrimSpace(id) == "" {
				return errors.New("--id is required")
			}

			secretValue := secret
			if fromStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read secret from stdin: %w", err)
				}
				secretValue = strings.TrimRight(string(data), "\n")
			}
			if secretValue == "" {
				return errors.New("secret value cannot be empty")
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			path := "/credentials/store/system/domain/_/createCredentials"
			if scopeVal == "folder" {
				encoded := jenkins.EncodeJobPath(folder)
				if encoded == "" {
					return errors.New("folder path required when scope=folder")
				}
				path = fmt.Sprintf("/%s/credentials/store/folder/domain/_/createCredentials", encoded)
			}

			body := map[string]any{
				"": "0",
				"credentials": map[string]any{
					"scope":       "GLOBAL",
					"id":          id,
					"description": description,
					"$class":      "org.jenkinsci.plugins.plaincredentials.impl.StringCredentialsImpl",
					"secret":      secretValue,
				},
			}

			resp, err := client.Do(client.NewRequest().SetBody(body), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("create credential failed: %s", resp.Status())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created credential %s in %s scope\n", id, scopeVal)
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "system", "Scope to create the credential (system or folder)")
	cmd.Flags().StringVar(&folder, "folder", "", "Folder path when scope=folder (e.g. team/service)")
	cmd.Flags().StringVar(&id, "id", "", "Credential identifier")
	cmd.Flags().StringVar(&description, "description", "", "Credential description")
	cmd.Flags().StringVar(&secret, "secret", "", "Secret value (omit to read from stdin with --from-stdin)")
	cmd.Flags().BoolVar(&fromStdin, "from-stdin", false, "Read secret value from standard input")

	return cmd
}

func newCredDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	var scope string
	var folder string
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scopeVal := strings.ToLower(strings.TrimSpace(scope))
			if scopeVal == "" {
				scopeVal = "system"
			}
			if scopeVal != "system" && scopeVal != "folder" {
				return fmt.Errorf("unsupported scope %q", scope)
			}

			credentialID := args[0]
			if strings.TrimSpace(credentialID) == "" {
				return errors.New("credential id required")
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			base := "/credentials/store/system/domain/_/credential"
			if scopeVal == "folder" {
				encoded := jenkins.EncodeJobPath(folder)
				if encoded == "" {
					return errors.New("folder path required when scope=folder")
				}
				base = fmt.Sprintf("/%s/credentials/store/folder/domain/_/credential", encoded)
			}

			path := fmt.Sprintf("%s/%s/doDelete", base, url.PathEscape(credentialID))
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("delete failed: %s", resp.Status())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted credential %s\n", credentialID)
			return nil
		},
	}
}
