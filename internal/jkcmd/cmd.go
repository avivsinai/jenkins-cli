package jkcmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/avivsinai/jenkins-cli/internal/build"
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

	if err := rootCmd.Execute(); err != nil {
		var exitErr *cmdutil.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Msg != "" {
				_, _ = fmt.Fprintln(ios.ErrOut, exitErr.Msg)
			}
			return exitErr.Code
		}
		if err != cmdutil.ErrSilent {
			_, _ = fmt.Fprintf(ios.ErrOut, "Error: %v\n", err)
		}
		return 1
	}

	return 0
}
