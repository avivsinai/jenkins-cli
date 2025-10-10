package cmdutil

import "errors"

var (
	// SilentError mirrors gh's sentinel used to suppress error printing.
	SilentError = errors.New("silent")
)

// ExitError wraps an exit code and optional message.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	return e.Msg
}
