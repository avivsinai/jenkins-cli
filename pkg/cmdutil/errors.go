package cmdutil

import "errors"

var (
	// ErrSilent mirrors gh's sentinel used to suppress error printing.
	ErrSilent = errors.New("silent")
)

// ExitError wraps an exit code and optional message.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	return e.Msg
}
