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
	return SelectPromptWithDefault(title, options, -1, true)
}

// SelectPromptWithDefault shows a selection prompt with a default option highlighted
// If defaultIdx is -1, no default is highlighted
// If allowCustom is true, adds option [0] for custom input
func SelectPromptWithDefault(title string, options []SelectOption, defaultIdx int, allowCustom bool) (string, error) {
	styles := DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Subtitle.Render(title))
	fmt.Println()

	for i, opt := range options {
		marker := " "
		if i == defaultIdx {
			marker = "→"
		}
		fmt.Printf("  %s %s %s\n",
			styles.Info.Render(marker),
			styles.Info.Render(fmt.Sprintf("[%d]", i+1)),
			styles.Text.Render(opt.Label))
	}

	if allowCustom {
		fmt.Printf("    %s %s\n",
			styles.Info.Render("[0]"),
			styles.Muted.Render("Enter custom value"))
	}

	fmt.Println()

	defaultHint := ""
	if defaultIdx >= 0 && defaultIdx < len(options) {
		defaultHint = fmt.Sprintf(" (default: %d)", defaultIdx+1)
	}

	minChoice := 0
	if !allowCustom {
		minChoice = 1
	}
	fmt.Printf("%s Enter your choice (%d-%d)%s: ",
		styles.Info.Render("→"),
		minChoice,
		len(options),
		defaultHint)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)

	// If empty input and there's a default, use default
	if input == "" && defaultIdx >= 0 && defaultIdx < len(options) {
		return options[defaultIdx].Value, nil
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < minChoice || choice > len(options) {
		return "", fmt.Errorf("invalid choice: %s", input)
	}

	if choice == 0 && allowCustom {
		return InputPrompt("Enter custom value")
	}

	return options[choice-1].Value, nil
}

// MultiSelectPrompt shows a multi-selection prompt where user can select multiple items
// Returns selected values
func MultiSelectPrompt(title string, options []SelectOption) ([]string, error) {
	styles := DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Subtitle.Render(title))
	fmt.Println(styles.Muted.Render("  Enter numbers separated by comma (e.g., 1,2,3) or 'all' for all"))
	fmt.Println()

	for i, opt := range options {
		fmt.Printf("  %s %s\n",
			styles.Info.Render(fmt.Sprintf("[%d]", i+1)),
			styles.Text.Render(opt.Label))
	}

	fmt.Println()
	fmt.Printf("%s Enter your choices: ", styles.Info.Render("→"))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("at least one selection is required")
	}

	// Handle 'all' keyword
	if strings.ToLower(input) == "all" {
		values := make([]string, len(options))
		for i, opt := range options {
			values[i] = opt.Value
		}
		return values, nil
	}

	// Parse comma-separated numbers
	parts := strings.Split(input, ",")
	var selected []string
	seen := make(map[string]bool)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		choice, err := strconv.Atoi(part)
		if err != nil || choice < 1 || choice > len(options) {
			return nil, fmt.Errorf("invalid choice: %s", part)
		}

		value := options[choice-1].Value
		if !seen[value] {
			selected = append(selected, value)
			seen[value] = true
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one selection is required")
	}

	return selected, nil
}

// InputPrompt shows a text input prompt and returns the entered value
func InputPrompt(prompt string) (string, error) {
	return InputPromptWithDefault(prompt, "")
}

// InputPromptWithDefault shows a text input prompt with a default value
// If the user enters nothing, the default value is returned
func InputPromptWithDefault(prompt string, defaultValue string) (string, error) {
	styles := DefaultStyles()

	defaultHint := ""
	if defaultValue != "" {
		defaultHint = styles.Muted.Render(fmt.Sprintf(" (default: %s)", defaultValue))
	}

	fmt.Printf("%s %s%s: ",
		styles.Info.Render("→"),
		styles.Text.Render(prompt),
		defaultHint)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		if defaultValue != "" {
			return defaultValue, nil
		}
		return "", fmt.Errorf("input cannot be empty")
	}

	return input, nil
}

// InputPromptOptional shows a text input prompt that allows empty values
func InputPromptOptional(prompt string, defaultValue string) (string, error) {
	styles := DefaultStyles()

	defaultHint := ""
	if defaultValue != "" {
		defaultHint = styles.Muted.Render(fmt.Sprintf(" (default: %s)", defaultValue))
	}

	fmt.Printf("%s %s%s: ",
		styles.Info.Render("→"),
		styles.Text.Render(prompt),
		defaultHint)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue, nil
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

// StepHeader prints a step header for multi-step TUI flows
func StepHeader(stepNum int, totalSteps int, title string) {
	styles := DefaultStyles()
	fmt.Println()
	fmt.Printf("%s %s\n",
		styles.Info.Render(fmt.Sprintf("[Step %d/%d]", stepNum, totalSteps)),
		styles.Subtitle.Render(title))
}

// WorkerSelectOption creates select options from worker list with name and ID
type WorkerSelectItem struct {
	Name     string
	WorkerID string
	Status   string
}

// AgentSelectItem represents an agent for selection
type AgentSelectItem struct {
	AgentID  string
	Hostname string
	Status   string
	GPUCount int
}

// GPUSelectItem represents a GPU for selection
type GPUSelectItem struct {
	GPUID  string
	Vendor string
	Model  string
	VRAMMb int64
}

// FormatWorkerOptions formats workers into select options
func FormatWorkerOptions(workers []WorkerSelectItem) []SelectOption {
	styles := DefaultStyles()
	var options []SelectOption

	for _, w := range workers {
		statusIcon := StatusIcon(w.Status)
		statusStyled := styles.StatusStyle(w.Status).Render(statusIcon)
		workerIDDisplay := w.WorkerID
		if len(workerIDDisplay) > 12 {
			workerIDDisplay = workerIDDisplay[:12] + "..."
		}
		label := fmt.Sprintf("%s %s (%s)",
			statusStyled,
			styles.Bold.Render(w.Name),
			styles.Muted.Render(workerIDDisplay))
		options = append(options, SelectOption{
			Label: label,
			Value: w.WorkerID,
		})
	}

	return options
}

// FormatAgentOptions formats agents into select options
func FormatAgentOptions(agents []AgentSelectItem) []SelectOption {
	styles := DefaultStyles()
	var options []SelectOption

	for _, a := range agents {
		statusIcon := StatusIcon(a.Status)
		statusStyled := styles.StatusStyle(a.Status).Render(statusIcon)
		agentIDDisplay := a.AgentID
		if len(agentIDDisplay) > 12 {
			agentIDDisplay = agentIDDisplay[:12] + "..."
		}
		label := fmt.Sprintf("%s %s (%s) - %d GPUs",
			statusStyled,
			styles.Bold.Render(a.Hostname),
			styles.Muted.Render(agentIDDisplay),
			a.GPUCount)
		options = append(options, SelectOption{
			Label: label,
			Value: a.AgentID,
		})
	}

	return options
}

// FormatGPUOptions formats GPUs into select options
func FormatGPUOptions(gpus []GPUSelectItem) []SelectOption {
	styles := DefaultStyles()
	var options []SelectOption

	for _, g := range gpus {
		label := fmt.Sprintf("%s - %s (%s, %dMB VRAM)",
			styles.Bold.Render(g.GPUID),
			g.Model,
			g.Vendor,
			g.VRAMMb)
		options = append(options, SelectOption{
			Label: label,
			Value: g.GPUID,
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
