package run

import (
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"strings"
	"testing"
)

func TestReviewPipelineLabelCannotEmitTerminalControls(t *testing.T) {
	label := pipelineLabel(shared.PipelineNode{Name: "checks\x1b[2J\x1b]52;c;Zm9v\a"})
	if strings.ContainsAny(label, "\x1b\a") {
		t.Fatalf("untrusted Pipeline label retains live terminal control bytes: %q", label)
	}
}
