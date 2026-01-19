package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// OutputFormat represents the output format type
type OutputFormat string

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatWide  OutputFormat = "wide"
)

// OutputConfig holds output configuration
type OutputConfig struct {
	Format OutputFormat
	Writer io.Writer
	Styles *Styles
}

// DefaultOutputConfig returns default output configuration
func DefaultOutputConfig() *OutputConfig {
	return &OutputConfig{
		Format: FormatTable,
		Writer: os.Stdout,
		Styles: DefaultStyles(),
	}
}

// Output handles rendering data in different formats
type Output struct {
	config *OutputConfig
}

// NewOutput creates a new output handler with default config
func NewOutput() *Output {
	return &Output{config: DefaultOutputConfig()}
}

// NewOutputWithFormat creates a new output handler with specified format
func NewOutputWithFormat(format OutputFormat) *Output {
	config := DefaultOutputConfig()
	config.Format = format
	return &Output{config: config}
}

// Format returns the current output format
func (o *Output) Format() OutputFormat {
	return o.config.Format
}

// SetFormat sets the output format
func (o *Output) SetFormat(format OutputFormat) *Output {
	o.config.Format = format
	return o
}

// SetWriter sets the output writer
func (o *Output) SetWriter(w io.Writer) *Output {
	o.config.Writer = w
	return o
}

// IsJSON returns true if the output format is JSON
func (o *Output) IsJSON() bool {
	return o.config.Format == FormatJSON
}

// PrintJSON outputs data as JSON
func (o *Output) PrintJSON(data interface{}) error {
	encoder := json.NewEncoder(o.config.Writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintTable outputs data as a styled table
func (o *Output) PrintTable(headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(o.config.Writer, o.config.Styles.Muted.Render("No items found"))
		return
	}
	fmt.Fprintln(o.config.Writer, SimpleTable(headers, rows))
}

// Print outputs data based on the configured format
// tableFunc is called for table format, data is used for JSON
func (o *Output) Print(data interface{}, tableFunc func()) error {
	if o.IsJSON() {
		return o.PrintJSON(data)
	}
	tableFunc()
	return nil
}

// Success prints a success message (only in table format)
func (o *Output) Success(message string) {
	if !o.IsJSON() {
		fmt.Fprintln(o.config.Writer, SuccessMessage(message))
	}
}

// Error prints an error message (only in table format)
func (o *Output) Error(message string) {
	if !o.IsJSON() {
		fmt.Fprintln(o.config.Writer, ErrorMessage(message))
	}
}

// Info prints an info message (only in table format)
func (o *Output) Info(message string) {
	if !o.IsJSON() {
		fmt.Fprintln(o.config.Writer, InfoMessage(message))
	}
}

// Warning prints a warning message (only in table format)
func (o *Output) Warning(message string) {
	if !o.IsJSON() {
		fmt.Fprintln(o.config.Writer, WarningMessage(message))
	}
}

// Println prints a line (only in table format)
func (o *Output) Println(a ...interface{}) {
	if !o.IsJSON() {
		fmt.Fprintln(o.config.Writer, a...)
	}
}

// Printf prints formatted output (only in table format)
func (o *Output) Printf(format string, a ...interface{}) {
	if !o.IsJSON() {
		fmt.Fprintf(o.config.Writer, format, a...)
	}
}

// ParseOutputFormat parses a string into an OutputFormat
func ParseOutputFormat(s string) OutputFormat {
	switch s {
	case "json":
		return FormatJSON
	case "wide":
		return FormatWide
	default:
		return FormatTable
	}
}

// ListResult is a generic result type for list commands
type ListResult[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// NewListResult creates a new list result
func NewListResult[T any](items []T) ListResult[T] {
	return ListResult[T]{
		Items: items,
		Total: len(items),
	}
}

// DetailResult wraps a single item for JSON output
type DetailResult[T any] struct {
	Item T `json:"item"`
}

// NewDetailResult creates a new detail result
func NewDetailResult[T any](item T) DetailResult[T] {
	return DetailResult[T]{Item: item}
}

// ActionResult represents the result of an action (create, update, delete)
type ActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
}

// NewActionResult creates a new action result
func NewActionResult(success bool, message string, id string) ActionResult {
	return ActionResult{
		Success: success,
		Message: message,
		ID:      id,
	}
}
