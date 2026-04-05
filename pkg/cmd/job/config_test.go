package job

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestJobConfigPath(t *testing.T) {
	path, err := jobConfigPath("platform/services/auth-relay")
	require.NoError(t, err)
	require.Equal(t, "/job/platform/job/services/job/auth-relay/config.xml", path)
}

func TestUpdateScriptPathInConfigReplacesExistingValue(t *testing.T) {
	input := `<root><factory class="org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory"><scriptPath>Jenkinsfile</scriptPath></factory></root>`
	output, err := updateScriptPathInConfig(input, "services/auth-relay/Jenkinsfile")
	require.NoError(t, err)
	require.Contains(t, output, `<scriptPath>services/auth-relay/Jenkinsfile</scriptPath>`)
	require.NotContains(t, output, `<scriptPath>Jenkinsfile</scriptPath>`)
}

func TestUpdateScriptPathInConfigInsertsMissingValueInsideFactory(t *testing.T) {
	input := `<root><factory class="org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory"></factory></root>`
	output, err := updateScriptPathInConfig(input, "services/auth-relay/Jenkinsfile")
	require.NoError(t, err)
	require.Contains(t, output, `<factory class="org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory">`)
	require.Contains(t, output, `<scriptPath>services/auth-relay/Jenkinsfile</scriptPath>`)
}

func TestUpdateScriptPathInConfigErrorsWithoutFactory(t *testing.T) {
	_, err := updateScriptPathInConfig(`<root/>`, "services/auth-relay/Jenkinsfile")
	require.Error(t, err)
	require.ErrorContains(t, err, "config.xml does not contain <factory class=")
}

func TestUpdateScriptPathInConfigRejectsWrongFactoryClass(t *testing.T) {
	input := `<root><factory class="hudson.model.FreeStyleProject"><scriptPath>Jenkinsfile</scriptPath></factory></root>`
	_, err := updateScriptPathInConfig(input, "services/auth-relay/Jenkinsfile")
	require.Error(t, err)
	require.ErrorContains(t, err, workflowBranchProjectFactoryClass)
}

func TestReadConfigXMLInput(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		path := t.TempDir() + "/config.xml"
		require.NoError(t, os.WriteFile(path, []byte("<root/>"), 0o600))

		cmd := &cobra.Command{}
		xml, source, err := readConfigXMLInput(cmd, path, false)
		require.NoError(t, err)
		require.Equal(t, "<root/>", xml)
		require.Equal(t, path, source)
	})

	t.Run("stdin", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader("<root/>"))

		xml, source, err := readConfigXMLInput(cmd, "", true)
		require.NoError(t, err)
		require.Equal(t, "<root/>", xml)
		require.Equal(t, "stdin", source)
	})

	t.Run("requires source", func(t *testing.T) {
		cmd := &cobra.Command{}
		_, _, err := readConfigXMLInput(cmd, "", false)
		require.Error(t, err)
		require.ErrorContains(t, err, "provide --file or --stdin")
	})

	t.Run("cannot combine file and stdin", func(t *testing.T) {
		cmd := &cobra.Command{}
		_, _, err := readConfigXMLInput(cmd, "config.xml", true)
		require.Error(t, err)
		require.ErrorContains(t, err, "cannot use --file with --stdin")
	})
}

func TestValidateJobScanResponse(t *testing.T) {
	t.Run("accepts redirect responses", func(t *testing.T) {
		err := validateJobScanResponse(
			"dogfood/jk-smoke",
			302,
			"302 Found",
			"",
		)
		require.NoError(t, err)
	})

	t.Run("surfaces non buildable job message", func(t *testing.T) {
		err := validateJobScanResponse(
			"dogfood/review-test",
			500,
			"500 Internal Server Error",
			"java.lang.IllegalStateException: item cannot be recomputed",
		)
		require.EqualError(t, err, "dogfood/review-test cannot be scanned: job has no buildable sources configured")
	})

	t.Run("keeps generic errors generic", func(t *testing.T) {
		err := validateJobScanResponse(
			"dogfood/jk-smoke",
			403,
			"403 Forbidden",
			"forbidden",
		)
		require.EqualError(t, err, "trigger scan for dogfood/jk-smoke failed: 403 Forbidden: forbidden")
	})
}

func TestResponseStatusErrorIncludesBodyDetail(t *testing.T) {
	err := responseStatusError("update config.xml for dogfood/auth", "400 Bad Request", "  invalid   plugin payload  ")
	require.EqualError(t, err, "update config.xml for dogfood/auth failed: 400 Bad Request: invalid plugin payload")
}
