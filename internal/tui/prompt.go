package tui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SelectOption represents an option in a selection prompt
type SelectOption struct {
	Label string
	Value string
}

// SelectPrompt shows an interactive selection prompt and returns the selected value
// Returns the selected option's value and any error
func SelectPrompt(title string, options []SelectOption) (string, error) {
	styles := DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Subtitle.Render(title))
	fmt.Println()

	for i, opt := range options {
		fmt.Printf("  %s %s\n",
			styles.Info.Render(fmt.Sprintf("[%d]", i+1)),
			styles.Text.Render(opt.Label))
	}

	// Add option for manual input
	fmt.Printf("  %s %s\n",
		styles.Info.Render("[0]"),
		styles.Muted.Render("Enter custom value"))

	fmt.Println()
	fmt.Printf("%s Enter your choice (0-%d): ",
		styles.Info.Render("→"),
		len(options))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	choice, err := strconv.Atoi(input)
	if err != nil || choice < 0 || choice > len(options) {
		return "", fmt.Errorf("invalid choice: %s", input)
	}

	if choice == 0 {
		return InputPrompt("Enter custom value")
	}

	return options[choice-1].Value, nil
}

// InputPrompt shows a text input prompt and returns the entered value
func InputPrompt(prompt string) (string, error) {
	styles := DefaultStyles()

	fmt.Printf("%s %s: ",
		styles.Info.Render("→"),
		styles.Text.Render(prompt))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("input cannot be empty")
	}

	return input, nil
}

// ConfirmPrompt shows a yes/no confirmation prompt
func ConfirmPrompt(message string) (bool, error) {
	styles := DefaultStyles()

	fmt.Printf("%s %s [y/N]: ",
		styles.Warning.Render("!"),
		styles.Text.Render(message))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == statusYes, nil
}

// WorkerSelectOption creates select options from worker list with name and ID
type WorkerSelectItem struct {
	Name     string
	WorkerID string
	Status   string
}

// FormatWorkerOptions formats workers into select options
func FormatWorkerOptions(workers []WorkerSelectItem) []SelectOption {
	styles := DefaultStyles()
	var options []SelectOption

	for _, w := range workers {
		statusIcon := StatusIcon(w.Status)
		statusStyled := styles.StatusStyle(w.Status).Render(statusIcon)
		label := fmt.Sprintf("%s %s (%s)",
			statusStyled,
			styles.Bold.Render(w.Name),
			styles.Muted.Render(w.WorkerID[:12]+"..."))
		options = append(options, SelectOption{
			Label: label,
			Value: w.WorkerID,
		})
	}

	return options
}

// FormatIPOptions formats IP addresses into select options
func FormatIPOptions(ips []string) []SelectOption {
	var options []SelectOption
	for _, ip := range ips {
		options = append(options, SelectOption{
			Label: ip,
			Value: ip,
		})
	}
	return options
}
