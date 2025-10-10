package cmd

import "fmt"

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("exit with code %d", e.code)
	}
	return e.msg
}

func newExitError(code int, msg string) error {
	return &exitError{code: code, msg: msg}
}
