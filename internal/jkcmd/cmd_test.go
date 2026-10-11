package jkcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/pkg/cmd/root"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
	"github.com/avivsinai/jenkins-cli/pkg/iostreams"
)

func TestExecutePrintsFatalErrorOnce(t *testing.T) {
	f := &cmdutil.Factory{ExecutableName: "jk", IOStreams: iostreams.System()}
	rootCmd, err := root.NewCmdRoot(f)
	if err != nil {
		t.Fatal(err)
	}
	rootCmd.AddCommand(&cobra.Command{
		Use:  "fail",
		Args: cobra.NoArgs,
		Run:  func(*cobra.Command, []string) {},
	})
	var out bytes.Buffer
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"fail", "extra"})

	if code := execute(rootCmd, &out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if n := strings.Count(out.String(), "Error:"); n != 1 {
		t.Fatalf("Error: printed %d times, want 1:\n%s", n, out.String())
	}
}
