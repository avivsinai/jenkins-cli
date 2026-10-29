package run

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func newRunStepsCmd(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "steps <jobPath> <buildNumber>",
		Short: "Inspect Pipeline stage and step IDs, labels and state",
		Long:  "Inspect stages and steps using the optional Pipeline Graph View plugin. Use a node ID with jk log --step to fetch its isolated log.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			build, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || build <= 0 {
				return fmt.Errorf("build number must be a positive integer")
			}
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}
			output, err := shared.FetchPipelineSteps(cmd.Context(), client, args[0], build)
			if err != nil {
				return err
			}
			return shared.PrintOutput(cmd, output, func() error {
				out := cmd.OutOrStdout()
				if _, err := fmt.Fprintln(out, "KIND\tID\tSTAGE\tSTATE\tLABEL"); err != nil {
					return err
				}
				var stages func([]shared.PipelineNode, string) error
				stages = func(nodes []shared.PipelineNode, indent string) error {
					for _, node := range nodes {
						if _, err := fmt.Fprintf(out, "stage\t%s\t\t%s\t%s%s\n", node.ID, node.State, indent, pipelineLabel(node)); err != nil {
							return err
						}
						if err := stages(node.Children, indent+"  "); err != nil {
							return err
						}
						if node.NextSibling != nil {
							if err := stages([]shared.PipelineNode{*node.NextSibling}, indent); err != nil {
								return err
							}
						}
					}
					return nil
				}
				if err := stages(output.Stages, ""); err != nil {
					return err
				}
				for _, step := range output.Steps {
					if _, err := fmt.Fprintf(out, "step\t%s\t%s\t%s\t%s\n", step.ID, step.StageID, step.State, pipelineLabel(step)); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
}

func pipelineLabel(node shared.PipelineNode) string {
	label := node.Name
	if label == "" {
		label = node.Title
	}
	return strings.Join(strings.Fields(label), " ")
}
