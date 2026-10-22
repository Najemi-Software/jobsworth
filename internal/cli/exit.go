package cli

import "errors"

// Exit codes shared by main and command entrypoints.
const (
	// ExitOK: nothing went wrong and no finding reached the fail threshold.
	ExitOK = 0
	// ExitFindings: the command ran to completion but reported findings that
	// reach the fail threshold.
	ExitFindings = 1
	// ExitUsage: bad invocation, unreadable input, bad config. Nothing was
	// validated.
	ExitUsage = 2
)

// ExitError is an error carrying a process exit code. Command entrypoints
// return it when the default (ExitUsage for parse/validate failures,
// ExitFindings for any other error) is not what they want. A nil Err means
// "exit with Code, print nothing" — the command already reported.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// Exit wraps err with an explicit exit code.
func Exit(code int, err error) error {
	return &ExitError{Code: code, Err: err}
}

// ExitCode extracts the code from an ExitError, or returns fallback for any
// other non-nil error. A nil error is ExitOK.
func ExitCode(err error, fallback int) int {
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return fallback
}
