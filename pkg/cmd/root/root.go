package root

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/build"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/artifact"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/auth"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/context"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/cred"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/job"
	logcmd "github.com/avivsinai/jenkins-cli/pkg/cmd/log"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/node"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/plugin"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/queue"
	runcmd "github.com/avivsinai/jenkins-cli/pkg/cmd/run"
	testcmd "github.com/avivsinai/jenkins-cli/pkg/cmd/test"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/version"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func NewCmdRoot(f *cmdutil.Factory) (*cobra.Command, error) {
	ios, err := f.Streams()
	if err != nil {
		return nil, err
	}

	root := &cobra.Command{
		Use:          f.ExecutableName,
		Short:        "jk is the Jenkins CLI for developers",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	root.SetContext(context.Background())

	root.PersistentFlags().StringP("context", "c", "", "Active Jenkins context name")
	root.PersistentFlags().Bool("json", false, "Output in JSON format when supported")
	root.PersistentFlags().Bool("yaml", false, "Output in YAML format when supported")

	root.AddCommand(
		auth.NewCmdAuth(f),
		contextcmd.NewCmdContext(f),
		job.NewCmdJob(f),
		cred.NewCmdCred(f),
		runcmd.NewCmdRun(f),
		logcmd.NewCmdLog(f),
		artifact.NewCmdArtifact(f),
		node.NewCmdNode(f),
		plugin.NewCmdPlugin(f),
		queue.NewCmdQueue(f),
		testcmd.NewCmdTest(f),
		version.NewCmdVersion(),
	)

	root.Version = build.Version
	root.SetOut(ios.Out)
	root.SetErr(ios.ErrOut)

	attachJSONHelp(root)

	return root, nil
}
