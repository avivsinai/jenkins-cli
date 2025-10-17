package shared

import (
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

func NewExitError(code int, msg string) error {
	return &cmdutil.ExitError{Code: code, Msg: msg}
}
