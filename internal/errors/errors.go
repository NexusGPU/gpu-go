// Package errors provides centralized error types for GPU Go.
// Keep it minimal - only add what's actually used.
package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for type checking with errors.Is()
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrBadRequest    = errors.New("bad request")
	ErrUnavailable   = errors.New("unavailable")
	ErrNotConfigured = errors.New("not configured")
)

// Error is a typed error with code, message and optional details
type Error struct {
	Code    string
	Message string
	Err     error
	Details map[string]any
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) Is(target error) bool {
	return errors.Is(e.Err, target)
}

// WithDetail adds a detail to the error (chainable)
func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// --- Error constructors (only what's actually used) ---

// Wrap wraps an error with a message
func Wrap(err error, message string) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:    "ERROR",
		Message: message,
		Err:     err,
	}
}

// NotFound creates a not found error
func NotFound(resource, id string) *Error {
	return &Error{
		Code:    "NOT_FOUND",
		Message: fmt.Sprintf("%s not found: %s", resource, id),
		Err:     ErrNotFound,
	}
}

// NotFoundf creates a not found error with formatted message
func NotFoundf(format string, args ...any) *Error {
	return &Error{
		Code:    "NOT_FOUND",
		Message: fmt.Sprintf(format, args...),
		Err:     ErrNotFound,
	}
}

// Conflict creates a conflict error
func Conflict(resource, reason string) *Error {
	return &Error{
		Code:    "CONFLICT",
		Message: fmt.Sprintf("%s conflict: %s", resource, reason),
		Err:     ErrConflict,
	}
}

// Unavailable creates a service unavailable error
func Unavailable(message string) *Error {
	return &Error{
		Code:    "UNAVAILABLE",
		Message: message,
		Err:     ErrUnavailable,
	}
}

// BadRequest creates a bad request error
func BadRequest(message string) *Error {
	return &Error{
		Code:    "BAD_REQUEST",
		Message: message,
		Err:     ErrBadRequest,
	}
}
