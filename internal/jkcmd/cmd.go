package jkcmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/avivsinai/jenkins-cli/internal/build"
	"github.com/spf13/cobra"

	jkfactory "github.com/avivsinai/jenkins-cli/pkg/cmd/factory"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/root"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func Main() int {
	f, err := jkfactory.New(build.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise factory: %v\n", err)
		return 1
	}

	ios, err := f.Streams()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to configure IO: %v\n", err)
		return 1
	}

	rootCmd, err := root.NewCmdRoot(f)
	if err != nil {
		_, _ = fmt.Fprintf(ios.ErrOut, "failed to create root command: %v\n", err)
		return 1
	}

	return execute(rootCmd, ios.ErrOut)
}

// execute runs rootCmd and reports a failure once on errOut. The root command
// silences cobra's own error printing so this is the only place that prints.
func execute(rootCmd *cobra.Command, errOut io.Writer) int {
	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmdutil.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Msg != "" {
				_, _ = fmt.Fprintln(errOut, exitErr.Msg)
			}
			return exitErr.Code
		}
		if err != cmdutil.ErrSilent {
			_, _ = fmt.Fprintf(errOut, "Error: %v\n", err)
		}
		return 1
	}

	return 0
}
