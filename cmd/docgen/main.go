// Command docgen generates skill rule files from the jk Cobra command tree.
//
// Usage:
//
//	go run ./cmd/docgen [-o skills/jk/rules]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/avivsinai/jenkins-cli/internal/docgen"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/root"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
	"github.com/avivsinai/jenkins-cli/pkg/iostreams"
)

func main() {
	outDir := flag.String("o", "skills/jk/rules", "Output directory for generated rule files")
	flag.Parse()

	ios, _, _, _ := iostreams.Test()
	f := &cmdutil.Factory{
		ExecutableName: "jk",
		IOStreams:      ios,
	}

	rootCmd, err := root.NewCmdRoot(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build command tree: %v\n", err)
		os.Exit(1)
	}

	if err := docgen.GenerateAll(rootCmd, "jk", *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Generated skill rules in %s\n", *outDir)
}
