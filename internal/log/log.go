// Package log provides a simple logging wrapper for GPU Go.
// Uses zerolog with console output and context enrichment.
package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger with context helpers
type Logger struct {
	zl zerolog.Logger
}

// Default is the default global logger
var Default = New(os.Stderr)

// New creates a new logger with console output
func New(w io.Writer) *Logger {
	output := zerolog.ConsoleWriter{
		Out:        w,
		TimeFormat: time.RFC3339,
	}
	return &Logger{
		zl: zerolog.New(output).With().Timestamp().Logger(),
	}
}

// SetVerbose enables/disables debug logging
func SetVerbose(verbose bool) {
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// --- Context enrichment ---

// WithComponent returns a logger with component field
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{zl: l.zl.With().Str("component", component).Logger()}
}

// WithAgentID returns a logger with agent_id field
func (l *Logger) WithAgentID(agentID string) *Logger {
	return &Logger{zl: l.zl.With().Str("agent_id", agentID).Logger()}
}

// WithWorkerID returns a logger with worker_id field
func (l *Logger) WithWorkerID(workerID string) *Logger {
	return &Logger{zl: l.zl.With().Str("worker_id", workerID).Logger()}
}

// --- Log levels ---

func (l *Logger) Debug() *Event { return &Event{e: l.zl.Debug()} }
func (l *Logger) Info() *Event  { return &Event{e: l.zl.Info()} }
func (l *Logger) Warn() *Event  { return &Event{e: l.zl.Warn()} }
func (l *Logger) Error() *Event { return &Event{e: l.zl.Error()} }

// Event wraps zerolog.Event for fluent API
type Event struct {
	e *zerolog.Event
}

func (e *Event) Str(key, val string) *Event {
	e.e = e.e.Str(key, val)
	return e
}

func (e *Event) Int(key string, val int) *Event {
	e.e = e.e.Int(key, val)
	return e
}

func (e *Event) Int64(key string, val int64) *Event {
	e.e = e.e.Int64(key, val)
	return e
}

func (e *Event) Bool(key string, val bool) *Event {
	e.e = e.e.Bool(key, val)
	return e
}

func (e *Event) Err(err error) *Event {
	e.e = e.e.Err(err)
	return e
}

func (e *Event) Dur(key string, d time.Duration) *Event {
	e.e = e.e.Dur(key, d)
	return e
}

func (e *Event) Msg(msg string) {
	e.e.Msg(msg)
}
