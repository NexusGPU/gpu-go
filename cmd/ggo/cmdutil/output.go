// Package cmdutil provides common utilities for CLI commands
package cmdutil

import (
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
)

// OutputFlag is the standard output format flag name
const OutputFlag = "output"

// AddOutputFlag adds the standard --output/-o flag to a command
func AddOutputFlag(cmd *cobra.Command, format *string) {
	cmd.PersistentFlags().StringVarP(format, OutputFlag, "o", "table", "Output format (table, json)")
}

// NewOutput creates a new Output instance from the format string
func NewOutput(format string) *tui.Output {
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(format))
}

// Renderable is an interface for types that can render themselves in both JSON and TUI formats
type Renderable interface {
	// RenderJSON returns the data structure for JSON output
	RenderJSON() any
	// RenderTUI renders the TUI output using the provided Output
	RenderTUI(out *tui.Output)
}

// Render outputs the data based on the format (JSON or TUI)
func Render(out *tui.Output, r Renderable) error {
	if out.IsJSON() {
		return out.PrintJSON(r.RenderJSON())
	}
	r.RenderTUI(out)
	return nil
}

// TableData represents data that can be rendered as a table
type TableData struct {
	Headers []string
	Rows    [][]string
	Empty   string // Message when no rows
}

func (t *TableData) RenderJSON() any {
	return t.Rows
}

func (t *TableData) RenderTUI(out *tui.Output) {
	if len(t.Rows) == 0 {
		if t.Empty != "" {
			out.Info(t.Empty)
		}
		return
	}
	table := tui.NewTable().Headers(t.Headers...).Rows(t.Rows)
	out.Println(table.String())
}

// StatusData represents key-value status information
type StatusData struct {
	Title string
	Items []StatusItem
	JSON  any // Custom JSON representation (optional)
}

// StatusItem represents a single status key-value pair
type StatusItem struct {
	Key    string
	Value  string
	Status string // Optional status for styling
}

func (s *StatusData) RenderJSON() any {
	if s.JSON != nil {
		return s.JSON
	}
	// Convert items to map for JSON
	result := make(map[string]string)
	for _, item := range s.Items {
		result[item.Key] = item.Value
	}
	return result
}

func (s *StatusData) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	if s.Title != "" {
		out.Println()
		out.Println(styles.Title.Render(s.Title))
		out.Println()
	}
	st := tui.NewStatusTable()
	for _, item := range s.Items {
		if item.Status != "" {
			st.AddWithStatus(item.Key, item.Value, item.Status)
		} else {
			st.Add(item.Key, item.Value)
		}
	}
	out.Println(st.String())
}

// ActionData represents the result of an action (create, update, delete, etc.)
type ActionData struct {
	Success bool
	Message string
	ID      string
}

func (a *ActionData) RenderJSON() any {
	return tui.NewActionResult(a.Success, a.Message, a.ID)
}

func (a *ActionData) RenderTUI(out *tui.Output) {
	if a.Success {
		out.Success(a.Message)
	} else {
		out.Error(a.Message)
	}
}

// ListData represents a list of items with table rendering
type ListData[T any] struct {
	Items   []T
	Headers []string
	RowFunc func(item T, styles *tui.Styles) []string // Convert item to row
	Empty   string
}

func (l *ListData[T]) RenderJSON() any {
	return tui.NewListResult(l.Items)
}

func (l *ListData[T]) RenderTUI(out *tui.Output) {
	if len(l.Items) == 0 {
		if l.Empty != "" {
			out.Info(l.Empty)
		}
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, item := range l.Items {
		rows = append(rows, l.RowFunc(item, styles))
	}

	table := tui.NewTable().Headers(l.Headers...).Rows(rows)
	out.Println(table.String())
}

// DetailData represents detailed information about a single item
type DetailData[T any] struct {
	Title    string
	Item     T
	ItemFunc func(item T, out *tui.Output) // Custom TUI rendering
}

func (d *DetailData[T]) RenderJSON() any {
	return tui.NewDetailResult(d.Item)
}

func (d *DetailData[T]) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	if d.Title != "" {
		out.Println()
		out.Println(styles.Title.Render(d.Title))
		out.Println()
	}
	if d.ItemFunc != nil {
		d.ItemFunc(d.Item, out)
	}
}

// ProgressData represents progress information (for operations like download)
type ProgressData struct {
	Operation string
	Target    string
	Progress  float64
	Total     int64
	Current   int64
}

func (p *ProgressData) RenderJSON() any {
	return map[string]any{
		"operation": p.Operation,
		"target":    p.Target,
		"progress":  p.Progress,
		"total":     p.Total,
		"current":   p.Current,
	}
}

func (p *ProgressData) RenderTUI(out *tui.Output) {
	if p.Total > 0 {
		out.Printf("\r  %s: %.1f%% (%d/%d bytes)", p.Operation, p.Progress, p.Current, p.Total)
	} else {
		out.Printf("%s %s...\n", p.Operation, p.Target)
	}
}

// CompositeData allows combining multiple renderables
type CompositeData struct {
	JSON  any          // JSON representation
	Parts []Renderable // TUI parts rendered in order
}

func (c *CompositeData) RenderJSON() any {
	return c.JSON
}

func (c *CompositeData) RenderTUI(out *tui.Output) {
	for _, part := range c.Parts {
		part.RenderTUI(out)
	}
}

// MessageData represents a simple message (success, error, info, warning)
type MessageData struct {
	Type    string // "success", "error", "info", "warning"
	Message string
	JSON    any // Optional custom JSON
}

func (m *MessageData) RenderJSON() any {
	if m.JSON != nil {
		return m.JSON
	}
	return map[string]string{"message": m.Message}
}

func (m *MessageData) RenderTUI(out *tui.Output) {
	switch m.Type {
	case "success":
		out.Success(m.Message)
	case "error":
		out.Error(m.Message)
	case "warning":
		out.Warning(m.Message)
	default:
		out.Info(m.Message)
	}
}
