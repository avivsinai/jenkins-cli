package search

import (
	"github.com/spf13/cobra"

	runcmd "github.com/avivsinai/jenkins-cli/pkg/cmd/run"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

// NewCmdSearch exposes run search as a top-level command for quick discovery.
func NewCmdSearch(f *cmdutil.Factory) *cobra.Command {
	cmd := runcmd.NewCmdRunSearch(f)
	cmd.Short = "Search Jenkins jobs and runs across folders"
	cmd.Long = `Search Jenkins jobs and their runs without knowing exact folder paths.

This is equivalent to 'jk run search', exposed at the top level for discoverability.`
	cmd.Example = `  # Discover job paths that contain "ada"
  jk search --job-glob "*ada*" --limit 5

  # Find recent failed builds across a folder
  jk search --folder ci-jobs --filter result=FAILURE --limit 10

  # Search for builds with specific parameter value
  jk search --job-glob "*/deploy-*" --filter param.ENVIRONMENT=production --since 7d`
	return cmd
}
