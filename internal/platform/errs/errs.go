// Package errs defines application-wide error kinds. The HTTP layer maps
// them to status codes, so domain code never has to know about HTTP.
package errs

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalid      = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrRateLimited  = errors.New("too many requests")
)

// Public wraps err with a message that is safe to show to end users.
// errors.Is/As still see the wrapped error.
func Public(err error, msg string) error {
	return &publicError{err: err, msg: msg}
}

// PublicMessage returns the user-facing message attached with Public, if any.
func PublicMessage(err error) (string, bool) {
	var pe *publicError
	if errors.As(err, &pe) {
		return pe.msg, true
	}
	return "", false
}

type publicError struct {
	err error
	msg string
}

func (e *publicError) Error() string { return e.msg + ": " + e.err.Error() }
func (e *publicError) Unwrap() error { return e.err }
