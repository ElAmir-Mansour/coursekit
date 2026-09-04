package cli

import (
	"errors"
	"strings"
)

// exitError carries a specific process exit code, so callers such as CI can
// distinguish "the tool failed" from "the course has problems".
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// withExitCode wraps an error so Execute returns a particular code.
func withExitCode(code int, err error) error {
	return &exitError{code: code, err: err}
}

func exitCodeFor(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// isUsageError reports whether cobra has already printed a helpful message.
func isUsageError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "accepts at most") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "requires at least")
}

// quietError is an error that Execute should not print, because the command
// already produced better output of its own. It exists purely to carry a
// non-zero exit code out to the shell.
type quietError struct{}

func (q *quietError) Error() string { return "" }

// isQuiet reports whether an error should be swallowed rather than printed.
func isQuiet(err error) bool {
	var q *quietError
	return errors.As(err, &q)
}
