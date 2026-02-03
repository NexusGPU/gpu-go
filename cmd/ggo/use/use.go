package use

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var paths = platform.DefaultPaths()

var (
	serverURL    string
	outputFormat string
)

// extractShortCode extracts the short code from a short link URL or returns the input as-is if it's already a code.
// Supports formats: "abc123", "https://go.gpu.tf/s/abc123", "go.gpu.tf/s/abc123"
func extractShortCode(input string) string {
	input = strings.TrimSpace(input)

	// If it looks like a URL, extract the last path segment
	if strings.Contains(input, "/") {
		parts := strings.Split(strings.TrimSuffix(input, "/"), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	return input
}

// NewUseCmd creates the use command
func NewUseCmd() *cobra.Command {
	var (
		longTerm  bool
		outputDir string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "use <share-link>",
		Short: "Set up a remote GPU environment",
		Long: `Set up a temporary or long-term connection to a remote GPU worker.

This command connects to a shared GPU worker and sets up the environment
so you can use the remote GPU as if it were local.

Examples:
  # Connect using short code (will prompt to activate)
  ggo use abc123

  # Connect using full short link
  ggo use https://go.gpu.tf/s/abc123

  # Directly activate without prompting
  ggo use abc123 -y

  # Set up a long-term GPU connection (persists across shell sessions)
  ggo use abc123 --long-term`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shortCode := extractShortCode(args[0])
			client := api.NewClient(api.WithBaseURL(serverURL))
			ctx := context.Background()
			out := getOutput()

			shareInfo, err := client.GetSharePublic(ctx, shortCode)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to get share info: error=%v", err)
				return err
			}

			klog.Infof("Found GPU worker: worker_id=%s vendor=%s connection_url=%s", shareInfo.WorkerID, shareInfo.HardwareVendor, shareInfo.ConnectionURL)

			if longTerm {
				return setupLongTermEnv(shareInfo, outputDir, yes, out)
			}
			return setupTemporaryEnv(shareInfo, yes, out)
		},
	}

	cmdutil.AddOutputFlag(cmd, &outputFormat)
	cmd.Flags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.Flags().BoolVar(&longTerm, "long-term", false, "Set up a long-term connection")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Output directory for configuration files")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Directly activate environment without prompting")

	return cmd
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

// NewCleanCmd creates the clean command
func NewCleanCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "clean [short-link]",
		Short: "Clean up remote GPU environment",
		Long: `Clean up temporary or long-term remote GPU environment setup.

Examples:
  # Clean up a specific connection (using code or link)
  ggo clean abc123
  ggo clean https://go.gpu.tf/s/abc123

  # Clean up all GPU Go connections
  ggo clean --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()
			if all {
				return cleanAllEnv(out)
			}

			if len(args) == 0 {
				return fmt.Errorf("short-link is required unless --all is specified")
			}

			shortCode := extractShortCode(args[0])
			return cleanEnv(shortCode, out)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Clean up all GPU Go connections")

	return cmd
}

// setupTemporaryEnv sets up a temporary GPU environment
func setupTemporaryEnv(shareInfo *api.SharePublicInfo, autoActivate bool, out *tui.Output) error {
	klog.Info("Setting up temporary GPU environment...")

	vendor := studio.ParseVendor(shareInfo.HardwareVendor)
	studioName := "current-os"

	// Create GPU environment config
	config := &studio.GPUEnvConfig{
		Vendor:        vendor,
		ConnectionURL: shareInfo.ConnectionURL,
		CachePath:     paths.CacheDir(),
		LogPath:       paths.StudioLogsDir(studioName),
		StudioName:    studioName,
		IsContainer:   false,
	}

	// Setup GPU environment (creates config files and directories)
	envResult, err := studio.SetupGPUEnv(paths, config)
	if err != nil {
		return fmt.Errorf("failed to setup GPU environment: %w", err)
	}

	if platform.IsWindows() {
		return renderWindowsEnv(shareInfo, config, envResult, autoActivate, out)
	}
	return renderUnixEnv(shareInfo, config, envResult, autoActivate, out)
}

// renderUnixEnv renders and optionally activates the Unix environment
func renderUnixEnv(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, autoActivate bool, out *tui.Output) error {
	// Generate environment script
	envScript, err := studio.GenerateEnvScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate env script: %w", err)
	}

	// Write env script to file
	envFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "env.sh")
	if err := os.WriteFile(envFile, []byte(envScript), 0755); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		out.Println()
		out.Success("GPU environment configured successfully!")
		out.Println()
		out.Printf("   Connection URL: %s\n", shareInfo.ConnectionURL)
		out.Printf("   Hardware:       %s\n", shareInfo.HardwareVendor)
		out.Printf("   Log Path:       %s\n", config.LogPath)
		out.Println()
	}

	// Check if we should auto-activate
	shouldActivate := autoActivate
	if !shouldActivate && !out.IsJSON() {
		out.Println(styles.Subtitle.Render("Activate Environment"))
		out.Println()
		out.Printf("Would you like to activate the GPU environment now? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		shouldActivate = response == "" || response == "y" || response == "yes"
	}

	if shouldActivate {
		return activateUnixEnv(envFile, envResult, out)
	}

	// Show manual activation instructions
	if !out.IsJSON() {
		out.Println()
		out.Println(styles.Subtitle.Render("Manual Activation"))
		out.Println()
		out.Println("To activate the environment, run:")
		out.Printf("\n   source %s\n\n", envFile)
		out.Println("Or copy-paste these exports:")
		out.Println()
		for k, v := range envResult.EnvVars {
			out.Printf("   export %s=\"%s\"\n", k, v)
		}
		// Add LD_LIBRARY_PATH and LD_PRELOAD
		out.Printf("   export LD_LIBRARY_PATH=\"%s:$LD_LIBRARY_PATH\"\n", config.CachePath)
		libNames := studio.GetLibraryNames(config.Vendor)
		if len(libNames) > 0 {
			var preloadPaths []string
			for _, lib := range libNames {
				preloadPaths = append(preloadPaths, filepath.Join(config.CachePath, lib))
			}
			out.Printf("   export LD_PRELOAD=\"%s\"\n", strings.Join(preloadPaths, ":"))
		}
		out.Println()
		out.Println("To clean up, run:")
		out.Println("\n   ggo clean")
		out.Println()
	}

	return out.Render(&tempEnvResultUnix{shareInfo: shareInfo, envFile: envFile, envResult: envResult})
}

// activateUnixEnv activates the environment in the current shell
func activateUnixEnv(envFile string, envResult *studio.GPUEnvResult, out *tui.Output) error {
	styles := tui.DefaultStyles()

	// Determine the current shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	out.Println()
	out.Printf("%s Activating GPU environment...\n", styles.Info.Render("◐"))
	out.Println()

	// For the current process, we can set env vars directly
	for k, v := range envResult.EnvVars {
		if err := os.Setenv(k, v); err != nil {
			klog.Warningf("Failed to set env var %s: %v", k, err)
		}
	}

	// Print what was set
	out.Println(styles.Success.Render("✓") + " Environment variables set in current process")
	out.Println()

	// To affect the parent shell, we need to tell the user to source or use exec
	out.Println(styles.Warning.Render("Note:") + " To activate in your current shell session, please run:")
	out.Printf("\n   source %s\n\n", envFile)

	// Alternative: spawn a new shell with the environment
	out.Println("Or start a new shell with the environment:")
	out.Printf("\n   %s --rcfile %s\n\n", filepath.Base(shell), envFile)

	// Check if the shell is zsh and provide zsh-specific instructions
	if strings.Contains(shell, "zsh") {
		out.Println("For zsh, you can also add to your .zshrc:")
		out.Printf("\n   echo 'source %s' >> ~/.zshrc\n\n", envFile)
	} else {
		out.Println("For bash, you can also add to your .bashrc:")
		out.Printf("\n   echo 'source %s' >> ~/.bashrc\n\n", envFile)
	}

	out.Println("To clean up, run:")
	out.Println("\n   ggo clean")
	out.Println()

	return nil
}

// renderWindowsEnv renders and optionally activates the Windows environment
func renderWindowsEnv(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, autoActivate bool, out *tui.Output) error {
	// Generate PowerShell script
	psScript, err := studio.GeneratePowerShellScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate PowerShell script: %w", err)
	}

	// Write scripts
	psFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "env.ps1")
	if err := os.WriteFile(psFile, []byte(psScript), 0644); err != nil {
		return fmt.Errorf("failed to write PowerShell file: %w", err)
	}

	// Generate batch script using studio package
	batScript, err := studio.GenerateBatchScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate batch script: %w", err)
	}
	batFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "env.bat")
	if err := os.WriteFile(batFile, []byte(batScript), 0644); err != nil {
		return fmt.Errorf("failed to write batch file: %w", err)
	}

	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		out.Println()
		out.Success("GPU environment configured successfully!")
		out.Println()
		out.Printf("   Connection URL: %s\n", shareInfo.ConnectionURL)
		out.Printf("   Hardware:       %s\n", shareInfo.HardwareVendor)
		out.Printf("   Log Path:       %s\n", config.LogPath)
		out.Println()

		if autoActivate {
			out.Println(styles.Info.Render("◐") + " Setting environment variables...")
			// Set for current process
			for k, v := range envResult.EnvVars {
				if err := os.Setenv(k, v); err != nil {
					klog.Warningf("Failed to set env var %s: %v", k, err)
				}
			}
			// Also set PATH with cache dir at front
			cachePath := config.CachePath
			if cachePath == "" {
				cachePath = paths.CacheDir()
			}
			if err := os.Setenv("PATH", cachePath+";"+os.Getenv("PATH")); err != nil {
				klog.Warningf("Failed to set PATH: %v", err)
			}
			if err := os.Setenv("CUDA_PATH", cachePath); err != nil {
				klog.Warningf("Failed to set CUDA_PATH: %v", err)
			}
			out.Println(styles.Success.Render("✓") + " Environment variables set")
			out.Println()
		}

		out.Println(styles.Subtitle.Render("Activation"))
		out.Println()
		out.Println("  PowerShell:")
		out.Printf("    . %s\n\n", psFile)
		out.Println("  CMD:")
		out.Printf("    %s\n\n", batFile)

		// Windows-specific: recommend ggo launch for reliable DLL loading
		out.Println(styles.Subtitle.Render("Recommended: Use ggo launch"))
		out.Println()
		out.Println(styles.Warning.Render("Note:") + " Windows loads System32 DLLs before PATH.")
		out.Println("For reliable GPU library loading, use:")
		out.Println()
		out.Println("  ggo launch python train.py")
		out.Println("  ggo launch jupyter notebook")
		out.Println()

		out.Println("To clean up, run:")
		out.Println("\n   ggo clean")
		out.Println()
	}

	return out.Render(&tempEnvResultWindows{shareInfo: shareInfo, psFile: psFile, batFile: batFile})
}

// tempEnvResultUnix implements Renderable for Unix temporary env setup
type tempEnvResultUnix struct {
	shareInfo *api.SharePublicInfo
	envFile   string
	envResult *studio.GPUEnvResult
}

func (r *tempEnvResultUnix) RenderJSON() any {
	return map[string]any{
		"success":       true,
		"env_file":      r.envFile,
		"env_vars":      r.envResult.EnvVars,
		"ld_so_conf":    r.envResult.LDSoConfPath,
		"ld_so_preload": r.envResult.LDSoPreloadPath,
		"share":         r.shareInfo,
	}
}

func (r *tempEnvResultUnix) RenderTUI(out *tui.Output) {
	// TUI output is handled in renderUnixEnv
}

// tempEnvResultWindows implements Renderable for Windows temporary env setup
type tempEnvResultWindows struct {
	shareInfo *api.SharePublicInfo
	psFile    string
	batFile   string
}

func (r *tempEnvResultWindows) RenderJSON() any {
	return map[string]any{
		"success":         true,
		"powershell_file": r.psFile,
		"batch_file":      r.batFile,
		"share":           r.shareInfo,
	}
}

func (r *tempEnvResultWindows) RenderTUI(out *tui.Output) {
	// TUI output is handled in renderWindowsEnv
}

// setupLongTermEnv sets up a long-term GPU environment
func setupLongTermEnv(shareInfo *api.SharePublicInfo, outputDir string, autoActivate bool, out *tui.Output) error {
	klog.Info("Setting up long-term GPU environment...")

	if outputDir == "" {
		outputDir = paths.UserDir()
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	vendor := studio.ParseVendor(shareInfo.HardwareVendor)
	studioName := "current-os"

	// Create GPU environment config
	config := &studio.GPUEnvConfig{
		Vendor:        vendor,
		ConnectionURL: shareInfo.ConnectionURL,
		CachePath:     paths.CacheDir(),
		LogPath:       paths.StudioLogsDir(studioName),
		StudioName:    studioName,
		IsContainer:   false,
	}

	// Setup GPU environment
	envResult, err := studio.SetupGPUEnv(paths, config)
	if err != nil {
		return fmt.Errorf("failed to setup GPU environment: %w", err)
	}

	// Write config file
	configFile := filepath.Join(outputDir, "config.json")
	configContent := fmt.Sprintf(`{
  "connection_url": "%s",
  "worker_id": "%s",
  "hardware_vendor": "%s",
  "platform": "%s"
}
`, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, runtime.GOOS)

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if platform.IsWindows() {
		return setupLongTermWindows(shareInfo, config, envResult, outputDir, out)
	}
	return setupLongTermUnix(shareInfo, config, envResult, outputDir, autoActivate, out)
}

// setupLongTermUnix sets up long-term environment for Unix
func setupLongTermUnix(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, outputDir string, autoActivate bool, out *tui.Output) error {
	// Generate the profile script
	profileScript, err := studio.GenerateEnvScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate profile script: %w", err)
	}

	profileSnippet := filepath.Join(outputDir, "profile.sh")
	if err := os.WriteFile(profileSnippet, []byte(profileScript), 0755); err != nil {
		return fmt.Errorf("failed to write profile snippet: %w", err)
	}

	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		out.Println()
		out.Success("Long-term GPU environment configured successfully!")
		out.Println()
		out.Printf("   Config directory: %s\n", outputDir)
		out.Printf("   Connection URL:   %s\n", shareInfo.ConnectionURL)
		out.Printf("   Hardware:         %s\n", shareInfo.HardwareVendor)
		out.Printf("   Log Path:         %s\n", config.LogPath)
		out.Println()
	}

	// Detect shell and offer to add to profile
	shell := os.Getenv("SHELL")
	shellRC := ""
	switch {
	case strings.Contains(shell, "zsh"):
		shellRC = filepath.Join(os.Getenv("HOME"), ".zshrc")
	case strings.Contains(shell, "bash"):
		shellRC = filepath.Join(os.Getenv("HOME"), ".bashrc")
	}

	if shellRC != "" && !out.IsJSON() {
		shouldAdd := autoActivate
		if !shouldAdd {
			out.Println(styles.Subtitle.Render("Permanent Activation"))
			out.Println()
			out.Printf("Add GPU environment to %s? [Y/n]: ", filepath.Base(shellRC))

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			shouldAdd = response == "" || response == "y" || response == "yes"
		}

		if shouldAdd {
			sourceLine := fmt.Sprintf("\n# GPU Go environment\nsource %s\n", profileSnippet)
			if err := appendToFile(shellRC, sourceLine, profileSnippet); err != nil {
				out.Warning(fmt.Sprintf("Failed to update %s: %v", shellRC, err))
			} else {
				out.Success(fmt.Sprintf("Added to %s", shellRC))
				out.Println()
				out.Println("Restart your terminal or run:")
				out.Printf("\n   source %s\n\n", shellRC)
			}
		}
	}

	if !out.IsJSON() {
		out.Println()
		out.Println(styles.Subtitle.Render("Manual Setup"))
		out.Println()
		out.Println("To activate in all new shells, add this to your shell profile:")
		out.Printf("\n   source %s\n\n", profileSnippet)
		out.Println("To clean up, run:")
		out.Println("\n   ggo clean --all")
		out.Println()
	}

	return out.Render(&longTermEnvResultUnix{shareInfo: shareInfo, outputDir: outputDir, envResult: envResult})
}

// appendToFile appends content to a file if it doesn't already contain the marker
func appendToFile(filePath, content, marker string) error {
	// Read existing content
	existing, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Check if already added
	if strings.Contains(string(existing), marker) {
		return nil // Already added
	}

	// Append
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	return err
}

// setupLongTermWindows sets up long-term environment for Windows
func setupLongTermWindows(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, outputDir string, out *tui.Output) error {
	// Generate PowerShell profile
	psProfile, err := studio.GeneratePowerShellScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate PowerShell profile: %w", err)
	}

	psProfilePath := filepath.Join(outputDir, "profile.ps1")
	if err := os.WriteFile(psProfilePath, []byte(psProfile), 0644); err != nil {
		return fmt.Errorf("failed to write PowerShell profile: %w", err)
	}

	// Generate batch file for setx
	var batContent strings.Builder
	batContent.WriteString("@echo off\n")
	batContent.WriteString("REM GPU Go long-term environment (CMD)\n")
	batContent.WriteString("REM This will set permanent user environment variables\n\n")

	for k, v := range envResult.EnvVars {
		batContent.WriteString(fmt.Sprintf("setx %s \"%s\"\n", k, v))
	}
	batContent.WriteString("\necho Environment variables set. Please restart your terminal.\n")

	batFile := filepath.Join(outputDir, "setenv.bat")
	if err := os.WriteFile(batFile, []byte(batContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write batch file: %w", err)
	}

	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		out.Println()
		out.Success("Long-term GPU environment configured successfully!")
		out.Println()
		out.Printf("   Config directory: %s\n", outputDir)
		out.Printf("   Connection URL:   %s\n", shareInfo.ConnectionURL)
		out.Printf("   Hardware:         %s\n", shareInfo.HardwareVendor)
		out.Printf("   Log Path:         %s\n", config.LogPath)
		out.Println()

		out.Println(styles.Subtitle.Render("Permanent Activation"))
		out.Println()
		out.Println("To activate in all new PowerShell sessions, add to your profile:")
		out.Println("  Run: notepad $PROFILE")
		out.Printf("  Add: . \"%s\"\n\n", psProfilePath)
		out.Println("Or set permanent environment variables:")
		out.Printf("  %s\n\n", batFile)
		out.Println("To clean up, run:")
		out.Println("\n   ggo clean --all")
		out.Println()
	}

	return out.Render(&longTermEnvResultWindows{shareInfo: shareInfo, outputDir: outputDir})
}

// longTermEnvResultUnix implements Renderable for Unix long-term env setup
type longTermEnvResultUnix struct {
	shareInfo *api.SharePublicInfo
	outputDir string
	envResult *studio.GPUEnvResult
}

func (r *longTermEnvResultUnix) RenderJSON() any {
	return map[string]any{
		"success":       true,
		"config_dir":    r.outputDir,
		"env_vars":      r.envResult.EnvVars,
		"ld_so_conf":    r.envResult.LDSoConfPath,
		"ld_so_preload": r.envResult.LDSoPreloadPath,
		"share":         r.shareInfo,
	}
}

func (r *longTermEnvResultUnix) RenderTUI(out *tui.Output) {
	// TUI output is handled in setupLongTermUnix
}

// longTermEnvResultWindows implements Renderable for Windows long-term env setup
type longTermEnvResultWindows struct {
	shareInfo *api.SharePublicInfo
	outputDir string
}

func (r *longTermEnvResultWindows) RenderJSON() any {
	return map[string]any{
		"success":    true,
		"config_dir": r.outputDir,
		"share":      r.shareInfo,
	}
}

func (r *longTermEnvResultWindows) RenderTUI(out *tui.Output) {
	// TUI output is handled in setupLongTermWindows
}

// cleanEnv cleans up a specific GPU environment
func cleanEnv(shortCode string, out *tui.Output) error {
	klog.Infof("Cleaning up GPU environment: short_link=%s", shortCode)

	tmpDirs, _ := filepath.Glob(paths.GlobPattern("gpugo-"))
	for _, dir := range tmpDirs {
		if err := os.RemoveAll(dir); err != nil {
			klog.Warningf("Failed to remove temp directory: dir=%s error=%v", dir, err)
		}
	}

	return out.Render(&cmdutil.ActionData{
		Success: true,
		Message: "GPU environment cleaned up successfully",
		ID:      shortCode,
	})
}

// cleanAllEnv cleans up all GPU environments
func cleanAllEnv(out *tui.Output) error {
	klog.Info("Cleaning up all GPU environments...")

	// Clean temporary directories
	tmpDirs, _ := filepath.Glob(paths.GlobPattern("gpugo-"))
	for _, dir := range tmpDirs {
		if err := os.RemoveAll(dir); err != nil {
			klog.Warningf("Failed to remove temp directory: dir=%s error=%v", dir, err)
		} else {
			klog.V(4).Infof("Removed temp directory: dir=%s", dir)
		}
	}

	// Clean current-os studio config
	currentOSConfigDir := paths.StudioConfigDir("current-os")
	if err := os.RemoveAll(currentOSConfigDir); err != nil {
		klog.Warningf("Failed to remove current-os config: error=%v", err)
	}

	// Try to remove source lines from shell profiles
	removeFromShellProfiles()

	return out.Render(&cleanAllResult{})
}

// removeFromShellProfiles removes GPU Go source lines from shell profiles
func removeFromShellProfiles() {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	profiles := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	}

	marker := "# GPU Go environment"
	for _, profile := range profiles {
		removeLineFromFile(profile, marker)
	}
}

// removeLineFromFile removes lines containing marker from a file
func removeLineFromFile(filePath, marker string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	var newLines []string
	skipNext := false

	for _, line := range lines {
		if strings.Contains(line, marker) {
			skipNext = true // Skip this line and the source line that follows
			continue
		}
		if skipNext && strings.HasPrefix(strings.TrimSpace(line), "source ") {
			skipNext = false
			continue
		}
		skipNext = false
		newLines = append(newLines, line)
	}

	newContent := strings.Join(newLines, "\n")
	if newContent != string(data) {
		_ = os.WriteFile(filePath, []byte(newContent), 0644)
	}
}

// cleanAllResult implements Renderable for clean all command
type cleanAllResult struct{}

func (r *cleanAllResult) RenderJSON() any {
	return tui.NewActionResult(true, "All GPU environments cleaned up successfully", "")
}

func (r *cleanAllResult) RenderTUI(out *tui.Output) {
	out.Println("All GPU environments cleaned up successfully!")
	out.Println()
	out.Println("Note: Environment variables in your current shell are not affected.")
	if platform.IsWindows() {
		out.Println("Start a new shell or run in PowerShell:")
		out.Println("  Remove-Item Env:TENSOR_FUSION_OPERATOR_CONNECTION_INFO, Env:TF_LOG_PATH, Env:TF_LOG_LEVEL, Env:TF_ENABLE_LOG")
	} else {
		out.Println("Start a new shell or run:")
		out.Println("  unset TENSOR_FUSION_OPERATOR_CONNECTION_INFO TF_LOG_PATH TF_LOG_LEVEL TF_ENABLE_LOG LD_PRELOAD")
	}
	out.Println()
}

// Silence unused import warning
var _ = exec.Command
