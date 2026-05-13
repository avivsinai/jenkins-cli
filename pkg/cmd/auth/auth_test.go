package auth

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func TestAuthLoginUsernameHelpMentionsSSOUserID(t *testing.T) {
	cmd := newAuthLoginCmd(&cmdutil.Factory{})

	flag := cmd.Flags().Lookup("username")
	require.NotNil(t, flag)
	require.Contains(t, flag.Usage, "Jenkins user ID")
	require.Contains(t, flag.Usage, "Google/SSO users")
}

func TestAuthLoginInteractivePromptsDisambiguateJenkinsCredentials(t *testing.T) {
	require.Contains(t, usernamePrompt, "Jenkins user ID")
	require.Contains(t, usernamePrompt, "email")
	require.Equal(t, "Jenkins API token", tokenPrompt)
}
