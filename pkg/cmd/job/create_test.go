package job

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/internal/secret"
)

func TestNormalizeMultibranchBitbucketSpec(t *testing.T) {
	spec, err := normalizeMultibranchBitbucketSpec(multibranchBitbucketSpec{
		Name:           " auth-relay ",
		Folder:         "/platform/services/",
		RepoOwner:      " playg ",
		Repository:     " my-service-repo ",
		ScriptPath:     " services/auth-relay/Jenkinsfile ",
		BitbucketURL:   "https://bitbucket.org/",
		BranchStrategy: "ALL",
	})
	require.NoError(t, err)
	require.Equal(t, "auth-relay", spec.Name)
	require.Equal(t, "platform/services", spec.Folder)
	require.Equal(t, "playg", spec.RepoOwner)
	require.Equal(t, "my-service-repo", spec.Repository)
	require.Equal(t, "services/auth-relay/Jenkinsfile", spec.ScriptPath)
	require.Equal(t, "https://bitbucket.org", spec.BitbucketURL)
	require.Equal(t, "all", spec.BranchStrategy)
}

func TestNormalizeMultibranchBitbucketSpecRejectsInvalidBranchStrategy(t *testing.T) {
	_, err := normalizeMultibranchBitbucketSpec(multibranchBitbucketSpec{
		Name:           "auth-relay",
		RepoOwner:      "playg",
		Repository:     "my-service-repo",
		ScriptPath:     "Jenkinsfile",
		BranchStrategy: "weird",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported --branch-strategy")
}

func TestCreateItemPath(t *testing.T) {
	require.Equal(t, "/createItem", createItemPath(""))
	require.Equal(t, "/job/platform/job/services/createItem", createItemPath("platform/services"))
}

func TestBuildSourcesXML(t *testing.T) {
	xml, err := buildSourcesXML(multibranchBitbucketSpec{
		RepoOwner:         "playg",
		Repository:        "my-service-repo",
		ScriptPath:        "services/auth-relay/Jenkinsfile",
		CredentialsID:     "bitbucket-ro",
		BitbucketURL:      "https://bitbucket.org",
		BranchStrategy:    "all",
		DiscoverOriginPRs: true,
		DiscoverForkPRs:   true,
	})
	require.NoError(t, err)
	require.Contains(t, xml, `<source class="com.cloudbees.jenkins.plugins.bitbucket.BitbucketSCMSource">`)
	require.Contains(t, xml, `<credentialsId>bitbucket-ro</credentialsId>`)
	require.Contains(t, xml, `<repoOwner>playg</repoOwner>`)
	require.Contains(t, xml, `<repository>my-service-repo</repository>`)
	require.Contains(t, xml, `<com.cloudbees.jenkins.plugins.bitbucket.BranchDiscoveryTrait><strategyId>3</strategyId></com.cloudbees.jenkins.plugins.bitbucket.BranchDiscoveryTrait>`)
	require.Contains(t, xml, `<com.cloudbees.jenkins.plugins.bitbucket.OriginPullRequestDiscoveryTrait><strategyId>1</strategyId></com.cloudbees.jenkins.plugins.bitbucket.OriginPullRequestDiscoveryTrait>`)
	require.Contains(t, xml, `TrustTeamForks`)
	require.Contains(t, xml, `<owner class="org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject" reference="../.."/>`)
	require.True(t, strings.Contains(xml, `</data><owner class="`), "owner must follow </data> inside sources")
}

func TestBuildSourcesXMLEscapesValues(t *testing.T) {
	xml, err := buildSourcesXML(multibranchBitbucketSpec{
		RepoOwner:      `playg & co`,
		Repository:     `svc<team>`,
		CredentialsID:  `bb"ro"&1`,
		BitbucketURL:   `https://bitbucket.example.com/root?a=1&b=2`,
		BranchStrategy: "all",
	})
	require.NoError(t, err)
	require.Contains(t, xml, `<repoOwner>playg &amp; co</repoOwner>`)
	require.Contains(t, xml, `<repository>svc&lt;team&gt;</repository>`)
	require.Contains(t, xml, `<credentialsId>bb&#34;ro&#34;&amp;1</credentialsId>`)
	require.Contains(t, xml, `<serverUrl>https://bitbucket.example.com/root?a=1&amp;b=2</serverUrl>`)
}

func TestPatchMultibranchConfig(t *testing.T) {
	// Uses the actual Jenkins default config format which does NOT include <description>.
	input := strings.TrimSpace(`
<?xml version='1.1' encoding='UTF-8'?>
<org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject plugin="workflow-multibranch@999">
  <properties/>
  <folderViews class="jenkins.branch.MultiBranchProjectViewHolder"/>
  <healthMetrics/>
  <icon class="jenkins.branch.MetadataActionFolderIcon"/>
  <orphanedItemStrategy class="com.cloudbees.hudson.plugins.folder.computed.DefaultOrphanedItemStrategy">
    <pruneDeadBranches>true</pruneDeadBranches>
    <daysToKeep>-1</daysToKeep>
    <numToKeep>-1</numToKeep>
  </orphanedItemStrategy>
  <triggers/>
  <disabled>false</disabled>
  <sources class="jenkins.branch.MultiBranchProject$BranchSourceList">
    <data/>
  </sources>
  <factory class="org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory">
    <scriptPath>Jenkinsfile</scriptPath>
  </factory>
</org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject>
`)

	output, err := patchMultibranchConfig(input, multibranchBitbucketSpec{
		Description:       "Auth relay pipeline",
		RepoOwner:         "playg",
		Repository:        "my-service-repo",
		ScriptPath:        "services/auth-relay/Jenkinsfile",
		CredentialsID:     "bitbucket-ro",
		BitbucketURL:      "https://bitbucket.org",
		BranchStrategy:    "all",
		DiscoverOriginPRs: true,
	})
	require.NoError(t, err)
	require.Contains(t, output, `<description>Auth relay pipeline</description>`)
	require.Contains(t, output, `<repoOwner>playg</repoOwner>`)
	require.Contains(t, output, `<repository>my-service-repo</repository>`)
	require.Contains(t, output, `<scriptPath>services/auth-relay/Jenkinsfile</scriptPath>`)
	require.Contains(t, output, `<folderViews class="jenkins.branch.MultiBranchProjectViewHolder"/>`)
}

func TestPatchMultibranchConfigWithExistingDescription(t *testing.T) {
	input := `<root><description>old</description><sources class="x"><data/></sources><factory class="y"><scriptPath>Jenkinsfile</scriptPath></factory></root>`
	output, err := patchMultibranchConfig(input, multibranchBitbucketSpec{
		Description:    "new desc",
		RepoOwner:      "playg",
		Repository:     "repo",
		ScriptPath:     "Jenkinsfile",
		BitbucketURL:   "https://bitbucket.org",
		BranchStrategy: "all",
	})
	require.NoError(t, err)
	require.Contains(t, output, `<description>new desc</description>`)
	require.NotContains(t, output, "old")
}

func TestPatchMultibranchConfigErrorsWhenRequiredSectionsMissing(t *testing.T) {
	_, err := patchMultibranchConfig(`<root><description/></root>`, multibranchBitbucketSpec{
		RepoOwner:      "playg",
		Repository:     "my-service-repo",
		ScriptPath:     "Jenkinsfile",
		BitbucketURL:   "https://bitbucket.org",
		BranchStrategy: "all",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "config.xml does not contain <sources>")
}

func TestCreateFollowupFailureIncludesCleanupHint(t *testing.T) {
	err := createFollowupFailure("platform/services/auth-relay", "update config.xml", errors.New("403 Forbidden"))
	require.ErrorContains(t, err, "partially created")
	require.ErrorContains(t, err, "Jenkins UI")
}

func TestReplaceElementPreservesDollarSigns(t *testing.T) {
	input := `<sources class="jenkins.branch.MultiBranchProject$BranchSourceList"><data/></sources>`
	replacement := `<sources class="jenkins.branch.MultiBranchProject$BranchSourceList"><data><new/></data></sources>`
	result, err := replaceElement(input, "sources", replacement)
	require.NoError(t, err)
	require.Contains(t, result, `MultiBranchProject$BranchSourceList`)
}

func TestCreateJobFromConfigXMLPostsToFolderCreateItem(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("KEYRING_BACKEND", "file")
	t.Setenv("KEYRING_FILE_DIR", tmp+"/secrets")
	t.Setenv("JK_ALLOW_INSECURE_STORE", "1")
	t.Setenv("JK_KEYRING_PASSPHRASE", "test-pass")
	t.Setenv("KEYRING_FILE_PASSWORD", "test-pass")

	store, err := secret.Open(secret.WithAllowFileFallback(true))
	require.NoError(t, err)
	require.NoError(t, store.Set(secret.TokenKey("test"), "token"))

	const configXML = "<?xml version='1.1' encoding='UTF-8'?><flow-definition/>"
	var got *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/crumbIssuer/api/json" {
			_, _ = w.Write([]byte(`{"crumb":"c","crumbRequestField":"Jenkins-Crumb"}`))
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		got = r
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.SetContext("test", &config.Context{URL: srv.URL, Username: "alice", AllowInsecureStore: true})
	client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
	require.NoError(t, err)

	result, err := createJobFromConfigXML(context.Background(), client, "platform/release", "amit-release", configXML, "stdin")
	require.NoError(t, err)
	require.Equal(t, "platform/release/amit-release", result.Path)

	require.NotNil(t, got)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "/job/platform/job/release/createItem", got.URL.Path)
	require.Equal(t, "amit-release", got.URL.Query().Get("name"))
	require.Equal(t, "application/xml", got.Header.Get("Content-Type"))
	require.Equal(t, "c", got.Header.Get("Jenkins-Crumb"))
	require.Equal(t, configXML, string(gotBody))
}
