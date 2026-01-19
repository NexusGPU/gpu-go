package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// TableBuilder provides a fluent API for building styled tables
type TableBuilder struct {
	styles  *Styles
	headers []string
	rows    [][]string
	widths  []int
}

// NewTable creates a new table builder with default styles
func NewTable() *TableBuilder {
	return &TableBuilder{
		styles: DefaultStyles(),
	}
}

// NewTableWithStyles creates a new table builder with custom styles
func NewTableWithStyles(styles *Styles) *TableBuilder {
	return &TableBuilder{
		styles: styles,
	}
}

// Headers sets the table headers
func (tb *TableBuilder) Headers(headers ...string) *TableBuilder {
	tb.headers = headers
	return tb
}

// Row adds a row to the table
func (tb *TableBuilder) Row(cells ...string) *TableBuilder {
	tb.rows = append(tb.rows, cells)
	return tb
}

// Rows adds multiple rows to the table
func (tb *TableBuilder) Rows(rows [][]string) *TableBuilder {
	tb.rows = append(tb.rows, rows...)
	return tb
}

// Widths sets column widths (optional)
func (tb *TableBuilder) Widths(widths ...int) *TableBuilder {
	tb.widths = widths
	return tb
}

// String renders the table as a string
func (tb *TableBuilder) String() string {
	if len(tb.headers) == 0 && len(tb.rows) == 0 {
		return ""
	}

	theme := tb.styles.Theme

	// Create table with custom styling
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(theme.TableBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return lipgloss.NewStyle().
					Foreground(theme.TableHeader).
					Bold(true).
					Padding(0, 1).
					Align(lipgloss.Center)
			case row%2 == 0:
				return lipgloss.NewStyle().
					Foreground(theme.TableRowEven).
					Padding(0, 1)
			default:
				return lipgloss.NewStyle().
					Foreground(theme.TableRowOdd).
					Padding(0, 1)
			}
		}).
		Headers(tb.headers...).
		Rows(tb.rows...)

	// Apply column widths if specified
	if len(tb.widths) > 0 {
		t = t.Width(sumWidths(tb.widths) + len(tb.widths)*3 + 1) // account for padding and borders
	}

	return t.String()
}

// sumWidths calculates total width
func sumWidths(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}
	return total
}

// SimpleTable creates a minimal table for quick output
func SimpleTable(headers []string, rows [][]string) string {
	return NewTable().Headers(headers...).Rows(rows).String()
}

// StatusTable creates a table optimized for status displays
type StatusTable struct {
	styles *Styles
	items  []StatusItem
}

// StatusItem represents a key-value status item
type StatusItem struct {
	Key    string
	Value  string
	Status string // optional: affects styling
}

// NewStatusTable creates a new status table
func NewStatusTable() *StatusTable {
	return &StatusTable{
		styles: DefaultStyles(),
	}
}

// Add adds a status item
func (st *StatusTable) Add(key, value string) *StatusTable {
	st.items = append(st.items, StatusItem{Key: key, Value: value})
	return st
}

// AddWithStatus adds a status item with status styling
func (st *StatusTable) AddWithStatus(key, value, status string) *StatusTable {
	st.items = append(st.items, StatusItem{Key: key, Value: value, Status: status})
	return st
}

// String renders the status table
func (st *StatusTable) String() string {
	if len(st.items) == 0 {
		return ""
	}

	var output string
	for _, item := range st.items {
		key := st.styles.Key.Render(item.Key + ":")
		var value string
		if item.Status != "" {
			value = st.styles.StatusStyle(item.Status).Render(item.Value)
		} else {
			value = st.styles.Value.Render(item.Value)
		}
		output += key + " " + value + "\n"
	}
	return output
}

// DetailBox creates a styled box for detail views
func DetailBox(title string, content string) string {
	styles := DefaultStyles()
	
	titleRendered := styles.Title.Render(title)
	box := styles.Box.Render(content)
	
	return titleRendered + "\n" + box
}

// SuccessMessage creates a styled success message
func SuccessMessage(message string) string {
	styles := DefaultStyles()
	icon := styles.Success.Render("✓")
	text := styles.Success.Render(message)
	return icon + " " + text
}

// ErrorMessage creates a styled error message
func ErrorMessage(message string) string {
	styles := DefaultStyles()
	icon := styles.Error.Render("✕")
	text := styles.Error.Render(message)
	return icon + " " + text
}

// WarningMessage creates a styled warning message
func WarningMessage(message string) string {
	styles := DefaultStyles()
	icon := styles.Warning.Render("!")
	text := styles.Warning.Render(message)
	return icon + " " + text
}

// InfoMessage creates a styled info message
func InfoMessage(message string) string {
	styles := DefaultStyles()
	icon := styles.Info.Render("ℹ")
	text := styles.Info.Render(message)
	return icon + " " + text
}

// Divider creates a styled horizontal divider
func Divider() string {
	styles := DefaultStyles()
	return styles.Muted.Render("─────────────────────────────────────────────────────")
}

// KeyValue renders a styled key-value pair
func KeyValue(key, value string) string {
	styles := DefaultStyles()
	return styles.Key.Render(key+":") + " " + styles.Value.Render(value)
}

// URL renders a styled URL
func URL(url string) string {
	styles := DefaultStyles()
	return styles.URL.Render(url)
}

// Code renders styled inline code
func Code(code string) string {
	styles := DefaultStyles()
	return styles.Code.Render(code)
}

// Bold renders bold text
func Bold(text string) string {
	styles := DefaultStyles()
	return styles.Bold.Render(text)
}

// Muted renders muted/dim text
func Muted(text string) string {
	styles := DefaultStyles()
	return styles.Muted.Render(text)
}
