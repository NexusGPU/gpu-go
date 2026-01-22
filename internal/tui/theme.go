// Package tui provides terminal UI styling and output formatting
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme defines the color palette for the TUI
type Theme struct {
	// Primary colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color

	// Text colors
	Text      lipgloss.Color
	TextMuted lipgloss.Color
	TextDim   lipgloss.Color

	// Status colors
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color
	Info    lipgloss.Color

	// Table colors
	TableBorder  lipgloss.Color
	TableHeader  lipgloss.Color
	TableRowOdd  lipgloss.Color
	TableRowEven lipgloss.Color
}

// DefaultTheme returns the default GPU Go theme
// Inspired by cyberpunk/neon aesthetics with a GPU/compute feel
func DefaultTheme() *Theme {
	return &Theme{
		// Primary: Vibrant cyan/teal (GPU/tech feel)
		Primary: lipgloss.Color("#00D9FF"),
		// Secondary: Electric purple
		Secondary: lipgloss.Color("#BD93F9"),
		// Accent: Hot magenta
		Accent: lipgloss.Color("#FF79C6"),

		// Text colors
		Text:      lipgloss.Color("#F8F8F2"),
		TextMuted: lipgloss.Color("#6272A4"),
		TextDim:   lipgloss.Color("#44475A"),

		// Status colors
		Success: lipgloss.Color("#50FA7B"),
		Warning: lipgloss.Color("#FFB86C"),
		Error:   lipgloss.Color("#FF5555"),
		Info:    lipgloss.Color("#8BE9FD"),

		// Table colors
		TableBorder:  lipgloss.Color("#6272A4"),
		TableHeader:  lipgloss.Color("#00D9FF"),
		TableRowOdd:  lipgloss.Color("#F8F8F2"),
		TableRowEven: lipgloss.Color("#6272A4"),
	}
}

// Styles contains all the pre-built lipgloss styles
type Styles struct {
	Theme *Theme

	// Title styles
	Title    lipgloss.Style
	Subtitle lipgloss.Style

	// Text styles
	Text  lipgloss.Style
	Muted lipgloss.Style
	Bold  lipgloss.Style

	// Status styles
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style

	// Component styles
	Key   lipgloss.Style
	Value lipgloss.Style
	URL   lipgloss.Style
	Code  lipgloss.Style

	// Box styles
	Box        lipgloss.Style
	SuccessBox lipgloss.Style
	ErrorBox   lipgloss.Style
	InfoBox    lipgloss.Style

	// Table styles
	TableHeader lipgloss.Style
	TableCell   lipgloss.Style
	TableBorder lipgloss.Style
}

// DefaultStyles returns styled components using the default theme
func DefaultStyles() *Styles {
	theme := DefaultTheme()
	return NewStyles(theme)
}

// NewStyles creates styled components from a theme
func NewStyles(theme *Theme) *Styles {
	s := &Styles{Theme: theme}

	// Title styles
	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Primary).
		MarginBottom(1)

	s.Subtitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Secondary)

	// Text styles
	s.Text = lipgloss.NewStyle().
		Foreground(theme.Text)

	s.Muted = lipgloss.NewStyle().
		Foreground(theme.TextMuted)

	s.Bold = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Text)

	// Status styles
	s.Success = lipgloss.NewStyle().
		Foreground(theme.Success)

	s.Warning = lipgloss.NewStyle().
		Foreground(theme.Warning)

	s.Error = lipgloss.NewStyle().
		Foreground(theme.Error)

	s.Info = lipgloss.NewStyle().
		Foreground(theme.Info)

	// Component styles
	s.Key = lipgloss.NewStyle().
		Foreground(theme.TextMuted).
		Width(16)

	s.Value = lipgloss.NewStyle().
		Foreground(theme.Text)

	s.URL = lipgloss.NewStyle().
		Foreground(theme.Accent).
		Underline(true)

	s.Code = lipgloss.NewStyle().
		Foreground(theme.Success).
		Background(lipgloss.Color("#282A36")).
		Padding(0, 1)

	// Box styles
	s.Box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.TableBorder).
		Padding(1, 2)

	s.SuccessBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Success).
		Padding(1, 2)

	s.ErrorBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Error).
		Padding(1, 2)

	s.InfoBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Info).
		Padding(1, 2)

	// Table styles
	s.TableHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TableHeader).
		Padding(0, 1)

	s.TableCell = lipgloss.NewStyle().
		Padding(0, 1)

	s.TableBorder = lipgloss.NewStyle().
		Foreground(theme.TableBorder)

	return s
}

// StatusStyle returns the appropriate style for a status string
func (s *Styles) StatusStyle(status string) lipgloss.Style {
	switch status {
	case "running", "active", "online", "connected", "enabled", "yes":
		return s.Success
	case "stopped", "inactive", "offline", "disconnected", "disabled", "no":
		return s.Muted
	case "error", "failed", "unhealthy":
		return s.Error
	case "starting", "stopping", "pending", "initializing":
		return s.Warning
	default:
		return s.Text
	}
}

// StatusIcon returns an icon for a status string
func StatusIcon(status string) string {
	switch status {
	case "running", "active", "online", "connected", "healthy":
		return "●"
	case "stopped", "inactive", "offline", "disconnected":
		return "○"
	case "error", "failed", "unhealthy":
		return "✕"
	case "starting", "stopping", "pending", "initializing":
		return "◐"
	case "enabled", "yes":
		return "✓"
	case "disabled", "no":
		return "✕"
	default:
		return "•"
	}
}
