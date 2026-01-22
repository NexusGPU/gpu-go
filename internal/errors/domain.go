// Package errors provides domain-specific error definitions for GPU Go.
// Only contains errors that are actually used in the codebase.
package errors

// --- Agent errors (used in internal/agent) ---

// ErrAgentNotRegistered indicates the agent is not registered
var ErrAgentNotRegistered = &Error{
	Code:    "AGENT_NOT_REGISTERED",
	Message: "agent is not registered, please run 'ggo agent register' first",
	Err:     ErrNotConfigured,
}
