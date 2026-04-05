package job

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

const (
	workflowMultiBranchMode = "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"
	defaultBitbucketURL     = "https://bitbucket.org"
)

type multibranchBitbucketSpec struct {
	Name              string
	Folder            string
	Description       string
	RepoOwner         string
	Repository        string
	ScriptPath        string
	CredentialsID     string
	BitbucketURL      string
	BranchStrategy    string
	DiscoverOriginPRs bool
	DiscoverForkPRs   bool
}

type jobCreateResult struct {
	Name              string `json:"name" yaml:"name"`
	Path              string `json:"path" yaml:"path"`
	Folder            string `json:"folder,omitempty" yaml:"folder,omitempty"`
	URL               string `json:"url,omitempty" yaml:"url,omitempty"`
	Type              string `json:"type" yaml:"type"`
	SCM               string `json:"scm" yaml:"scm"`
	RepoOwner         string `json:"repoOwner" yaml:"repoOwner"`
	Repository        string `json:"repository" yaml:"repository"`
	ScriptPath        string `json:"scriptPath" yaml:"scriptPath"`
	BitbucketURL      string `json:"bitbucketUrl" yaml:"bitbucketUrl"`
	CredentialsID     string `json:"credentialsId,omitempty" yaml:"credentialsId,omitempty"`
	BranchStrategy    string `json:"branchStrategy" yaml:"branchStrategy"`
	DiscoverOriginPRs bool   `json:"discoverOriginPRs,omitempty" yaml:"discoverOriginPRs,omitempty"`
	DiscoverForkPRs   bool   `json:"discoverForkPRs,omitempty" yaml:"discoverForkPRs,omitempty"`
}

func newJobCreateCmd(f *cmdutil.Factory) *cobra.Command {
	opts := multibranchBitbucketSpec{
		ScriptPath:     "Jenkinsfile",
		BitbucketURL:   defaultBitbucketURL,
		BranchStrategy: "all",
	}

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Jenkins job",
		Long: "Create a Jenkins job.\n\n" +
			"Current support is intentionally focused: this command creates a Multibranch\n" +
			"Pipeline backed by a Bitbucket repository and configures the Jenkinsfile path.\n\n" +
			"If Jenkins creates the job but a later config.xml step fails, the partially\n" +
			"created job remains and may need cleanup via the Jenkins UI.",
		Example: `  jk job create auth-relay \
    --folder platform/services \
    --repo-owner playg \
    --repository taboola-sales-skills \
    --script-path services/auth-relay/Jenkinsfile \
    --credentials bitbucket-readonly`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.ValidateOutputFlags(cmd); err != nil {
				return err
			}

			opts.Name = strings.TrimSpace(args[0])
			spec, err := normalizeMultibranchBitbucketSpec(opts)
			if err != nil {
				return err
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			result, err := createMultibranchBitbucketJob(cmd.Context(), client, spec)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, result, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", result.Path)
				if result.URL != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", result.URL)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "SCM: Bitbucket %s/%s\n", result.RepoOwner, result.Repository)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Script Path: %s\n", result.ScriptPath)
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&opts.Folder, "folder", "", "Folder path where the job should be created")
	cmd.Flags().StringVar(&opts.Description, "description", "", "Job description")
	cmd.Flags().StringVar(&opts.RepoOwner, "repo-owner", "", "Bitbucket repository owner/workspace")
	cmd.Flags().StringVar(&opts.Repository, "repository", "", "Bitbucket repository name")
	cmd.Flags().StringVar(&opts.ScriptPath, "script-path", opts.ScriptPath, "Path to the Jenkinsfile inside the repository")
	cmd.Flags().StringVar(&opts.CredentialsID, "credentials", "", "Jenkins credentials ID for the Bitbucket source")
	cmd.Flags().StringVar(&opts.BitbucketURL, "bitbucket-url", opts.BitbucketURL, "Bitbucket server URL")
	cmd.Flags().StringVar(&opts.BranchStrategy, "branch-strategy", opts.BranchStrategy, "Branch discovery strategy: all, exclude-prs, only-prs")
	cmd.Flags().BoolVar(&opts.DiscoverOriginPRs, "discover-origin-prs", false, "Discover pull requests raised from the origin repository")
	cmd.Flags().BoolVar(&opts.DiscoverForkPRs, "discover-fork-prs", false, "Discover pull requests raised from forks using TrustTeamForks")

	return cmd
}

func normalizeMultibranchBitbucketSpec(spec multibranchBitbucketSpec) (multibranchBitbucketSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Folder = strings.Trim(strings.TrimSpace(spec.Folder), "/")
	spec.Description = strings.TrimSpace(spec.Description)
	spec.RepoOwner = strings.TrimSpace(spec.RepoOwner)
	spec.Repository = strings.TrimSpace(spec.Repository)
	spec.ScriptPath = strings.TrimSpace(spec.ScriptPath)
	spec.CredentialsID = strings.TrimSpace(spec.CredentialsID)
	spec.BitbucketURL = strings.TrimRight(strings.TrimSpace(spec.BitbucketURL), "/")
	spec.BranchStrategy = strings.ToLower(strings.TrimSpace(spec.BranchStrategy))

	switch {
	case spec.Name == "":
		return spec, errors.New("job name is required")
	case spec.RepoOwner == "":
		return spec, errors.New("--repo-owner is required")
	case spec.Repository == "":
		return spec, errors.New("--repository is required")
	case spec.ScriptPath == "":
		return spec, errors.New("--script-path is required")
	case spec.BitbucketURL == "":
		spec.BitbucketURL = defaultBitbucketURL
	}

	if spec.BranchStrategy == "" {
		spec.BranchStrategy = "all"
	}

	if _, err := branchStrategyID(spec.BranchStrategy); err != nil {
		return spec, err
	}

	return spec, nil
}

func createMultibranchBitbucketJob(ctx context.Context, client *jenkins.Client, spec multibranchBitbucketSpec) (*jobCreateResult, error) {
	createPath := createItemPath(spec.Folder)
	createReq := client.NewRequest().
		SetContext(ctx).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetQueryParam("name", spec.Name).
		SetQueryParam("mode", workflowMultiBranchMode)

	resp, err := client.Do(createReq, http.MethodPost, createPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create multibranch project: %w", err)
	}
	if resp.StatusCode() >= 400 {
		return nil, responseStatusError("create multibranch project", resp.Status(), resp.String())
	}

	jobPath := fullJobPath(spec.Folder, spec.Name)
	configPath := fmt.Sprintf("/%s/config.xml", jenkins.EncodeJobPath(jobPath))

	configResp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetHeader("Accept", "application/xml"),
		http.MethodGet,
		configPath,
		nil,
	)
	if err != nil {
		return nil, createFollowupFailure(jobPath, "fetch default config.xml", err)
	}
	if configResp.StatusCode() >= 400 {
		return nil, createFollowupFailure(jobPath, "fetch default config.xml", responseStatusError("fetch default config.xml", configResp.Status(), configResp.String()))
	}

	updatedConfig, err := patchMultibranchConfig(configResp.String(), spec)
	if err != nil {
		return nil, fmt.Errorf("prepare config.xml for %s: %w", jobPath, err)
	}

	updateResp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetHeader("Accept", "application/xml").
			SetHeader("Content-Type", "application/xml").
			SetBody(updatedConfig),
		http.MethodPost,
		configPath,
		nil,
	)
	if err != nil {
		return nil, createFollowupFailure(jobPath, "update config.xml", err)
	}
	if updateResp.StatusCode() >= 400 {
		return nil, createFollowupFailure(jobPath, "update config.xml", responseStatusError("update config.xml", updateResp.Status(), updateResp.String()))
	}

	return &jobCreateResult{
		Name:              spec.Name,
		Path:              jobPath,
		Folder:            spec.Folder,
		URL:               jobURL(client, jobPath),
		Type:              "multibranch",
		SCM:               "bitbucket",
		RepoOwner:         spec.RepoOwner,
		Repository:        spec.Repository,
		ScriptPath:        spec.ScriptPath,
		BitbucketURL:      spec.BitbucketURL,
		CredentialsID:     spec.CredentialsID,
		BranchStrategy:    spec.BranchStrategy,
		DiscoverOriginPRs: spec.DiscoverOriginPRs,
		DiscoverForkPRs:   spec.DiscoverForkPRs,
	}, nil
}

func createItemPath(folder string) string {
	if folder == "" {
		return "/createItem"
	}
	return fmt.Sprintf("/%s/createItem", jenkins.EncodeJobPath(folder))
}

func fullJobPath(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}

func jobURL(client *jenkins.Client, jobPath string) string {
	if client == nil || client.Context() == nil || strings.TrimSpace(client.Context().URL) == "" {
		return ""
	}
	return strings.TrimRight(client.Context().URL, "/") + "/" + jenkins.EncodeJobPath(jobPath) + "/"
}

func patchMultibranchConfig(configXML string, spec multibranchBitbucketSpec) (string, error) {
	sourcesXML, err := buildSourcesXML(spec)
	if err != nil {
		return "", err
	}

	descriptionXML := fmt.Sprintf("<description>%s</description>", xmlEscape(spec.Description))
	configXML, err = replaceOrInsertElement(configXML, "description", descriptionXML)
	if err != nil {
		return "", err
	}
	configXML, err = replaceElement(configXML, "sources", sourcesXML)
	if err != nil {
		return "", err
	}
	configXML, err = replaceElement(configXML, "factory", buildFactoryXML(spec.ScriptPath))
	if err != nil {
		return "", err
	}

	return configXML, nil
}

func buildSourcesXML(spec multibranchBitbucketSpec) (string, error) {
	branchStrategy, err := branchStrategyID(spec.BranchStrategy)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(`<sources class="jenkins.branch.MultiBranchProject$BranchSourceList">`)
	b.WriteString(`<data><jenkins.branch.BranchSource>`)
	b.WriteString(`<source class="com.cloudbees.jenkins.plugins.bitbucket.BitbucketSCMSource">`)
	b.WriteString("<id>")
	b.WriteString(uuid.NewString())
	b.WriteString("</id>")
	b.WriteString("<serverUrl>")
	b.WriteString(xmlEscape(spec.BitbucketURL))
	b.WriteString("</serverUrl>")
	if spec.CredentialsID != "" {
		b.WriteString("<credentialsId>")
		b.WriteString(xmlEscape(spec.CredentialsID))
		b.WriteString("</credentialsId>")
	}
	b.WriteString("<repoOwner>")
	b.WriteString(xmlEscape(spec.RepoOwner))
	b.WriteString("</repoOwner>")
	b.WriteString("<repository>")
	b.WriteString(xmlEscape(spec.Repository))
	b.WriteString("</repository>")
	b.WriteString("<traits>")
	fmt.Fprintf(&b, `<com.cloudbees.jenkins.plugins.bitbucket.BranchDiscoveryTrait><strategyId>%d</strategyId></com.cloudbees.jenkins.plugins.bitbucket.BranchDiscoveryTrait>`, branchStrategy)
	if spec.DiscoverOriginPRs {
		b.WriteString(`<com.cloudbees.jenkins.plugins.bitbucket.OriginPullRequestDiscoveryTrait><strategyId>1</strategyId></com.cloudbees.jenkins.plugins.bitbucket.OriginPullRequestDiscoveryTrait>`)
	}
	if spec.DiscoverForkPRs {
		b.WriteString(`<com.cloudbees.jenkins.plugins.bitbucket.ForkPullRequestDiscoveryTrait><strategyId>1</strategyId><trust class="com.cloudbees.jenkins.plugins.bitbucket.ForkPullRequestDiscoveryTrait$TrustTeamForks"/></com.cloudbees.jenkins.plugins.bitbucket.ForkPullRequestDiscoveryTrait>`)
	}
	b.WriteString("</traits>")
	b.WriteString(`</source>`)
	b.WriteString(`<strategy class="jenkins.branch.DefaultBranchPropertyStrategy"><properties class="empty-list"/></strategy>`)
	b.WriteString(`</jenkins.branch.BranchSource></data></sources>`)

	return b.String(), nil
}

func buildFactoryXML(scriptPath string) string {
	return fmt.Sprintf(
		`<factory class="org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory"><scriptPath>%s</scriptPath></factory>`,
		xmlEscape(scriptPath),
	)
}

func branchStrategyID(strategy string) (int, error) {
	// Bitbucket BranchDiscoveryTrait strategyId values: 1=exclude-prs, 2=only-prs, 3=all.
	switch strategy {
	case "exclude-prs":
		return 1, nil
	case "only-prs":
		return 2, nil
	case "all":
		return 3, nil
	default:
		return 0, fmt.Errorf("unsupported --branch-strategy %q (valid: all, exclude-prs, only-prs)", strategy)
	}
}

func createFollowupFailure(jobPath, step string, err error) error {
	return fmt.Errorf("%s for %s: %w; the job may have been partially created — check the Jenkins UI", step, jobPath, err)
}
