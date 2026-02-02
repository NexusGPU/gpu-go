package use

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
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
	var longTerm bool
	var outputDir string

	cmd := &cobra.Command{
		Use:   "use <short-link>",
		Short: "Set up a remote GPU environment",
		Long: `Set up a temporary or long-term connection to a remote GPU worker.

This command connects to a shared GPU worker and sets up the environment
so you can use the remote GPU as if it were local.

Examples:
  # Connect using short code
  ggo use abc123

  # Connect using full short link
  ggo use https://go.gpu.tf/s/abc123

  # Set up a long-term GPU connection
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
				return setupLongTermEnv(shareInfo, outputDir, out)
			}
			return setupTemporaryEnv(shareInfo, out)
		},
	}

	cmdutil.AddOutputFlag(cmd, &outputFormat)
	cmd.Flags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.Flags().BoolVar(&longTerm, "long-term", false, "Set up a long-term connection")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Output directory for configuration files")

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
func setupTemporaryEnv(shareInfo *api.SharePublicInfo, out *tui.Output) error {
	klog.Info("Setting up temporary GPU environment...")

	tmpDir, err := os.MkdirTemp("", "gpugo-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	connFile := filepath.Join(tmpDir, "connection.txt")
	if err := os.WriteFile(connFile, []byte(shareInfo.ConnectionURL), 0644); err != nil {
		return fmt.Errorf("failed to write connection file: %w", err)
	}

	if platform.IsWindows() {
		return out.Render(&tempEnvResultWindows{shareInfo: shareInfo, tmpDir: tmpDir})
	}
	return out.Render(&tempEnvResultUnix{shareInfo: shareInfo, tmpDir: tmpDir})
}

// tempEnvResultUnix implements Renderable for Unix temporary env setup
type tempEnvResultUnix struct {
	shareInfo *api.SharePublicInfo
	tmpDir    string
}

func (r *tempEnvResultUnix) RenderJSON() any {
	return map[string]any{
		"success": true,
		"tmp_dir": r.tmpDir,
		"share":   r.shareInfo,
	}
}

func (r *tempEnvResultUnix) RenderTUI(out *tui.Output) {
	envFile := filepath.Join(r.tmpDir, "env.sh")
	envContent := fmt.Sprintf(`#!/bin/bash
# GPU Go temporary environment
export GPU_GO_CONNECTION_URL="%s"
export GPU_GO_WORKER_ID="%s"
export GPU_GO_VENDOR="%s"
export GPU_GO_TMP_DIR="%s"

# CUDA visibility (if nvidia)
if [ "%s" = "nvidia" ]; then
    export CUDA_VISIBLE_DEVICES=0
fi

echo "GPU Go environment activated!"
echo "Connection URL: %s"
echo ""
echo "To deactivate, run: ggo clean"
`, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.tmpDir,
		r.shareInfo.HardwareVendor, r.shareInfo.ConnectionURL)

	if err := os.WriteFile(envFile, []byte(envContent), 0755); err != nil {
		out.Error(fmt.Sprintf("Failed to write env file: %v", err))
		return
	}

	activateScript := filepath.Join(r.tmpDir, "activate")
	if err := os.WriteFile(activateScript, []byte("source "+envFile), 0755); err != nil {
		out.Error(fmt.Sprintf("Failed to write activate script: %v", err))
		return
	}

	out.Println()
	out.Println("Temporary GPU environment set up successfully!")
	out.Println()
	out.Printf("   Connection URL: %s\n", r.shareInfo.ConnectionURL)
	out.Printf("   Hardware:       %s\n", r.shareInfo.HardwareVendor)
	out.Println()
	out.Println("To activate the environment, run:")
	out.Printf("\n   source %s\n\n", envFile)
	out.Println("To clean up, run:")
	out.Println("\n   ggo clean")
	out.Println()
}

// tempEnvResultWindows implements Renderable for Windows temporary env setup
type tempEnvResultWindows struct {
	shareInfo *api.SharePublicInfo
	tmpDir    string
}

func (r *tempEnvResultWindows) RenderJSON() any {
	return map[string]any{
		"success": true,
		"tmp_dir": r.tmpDir,
		"share":   r.shareInfo,
	}
}

func (r *tempEnvResultWindows) RenderTUI(out *tui.Output) {
	psFile := filepath.Join(r.tmpDir, "env.ps1")
	psContent := fmt.Sprintf(`# GPU Go temporary environment (PowerShell)
$env:GPU_GO_CONNECTION_URL = "%s"
$env:GPU_GO_WORKER_ID = "%s"
$env:GPU_GO_VENDOR = "%s"
$env:GPU_GO_TMP_DIR = "%s"

# CUDA visibility (if nvidia)
if ($env:GPU_GO_VENDOR -eq "nvidia") {
    $env:CUDA_VISIBLE_DEVICES = "0"
}

Write-Host "GPU Go environment activated!"
Write-Host "Connection URL: %s"
Write-Host ""
Write-Host "To deactivate, run: ggo clean"
`, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.tmpDir,
		r.shareInfo.ConnectionURL)

	if err := os.WriteFile(psFile, []byte(psContent), 0644); err != nil {
		out.Error(fmt.Sprintf("Failed to write PowerShell env file: %v", err))
		return
	}

	batFile := filepath.Join(r.tmpDir, "env.bat")
	batContent := fmt.Sprintf(`@echo off
REM GPU Go temporary environment (CMD)
set GPU_GO_CONNECTION_URL=%s
set GPU_GO_WORKER_ID=%s
set GPU_GO_VENDOR=%s
set GPU_GO_TMP_DIR=%s

REM CUDA visibility (if nvidia)
if "%%GPU_GO_VENDOR%%"=="nvidia" set CUDA_VISIBLE_DEVICES=0

echo GPU Go environment activated!
echo Connection URL: %s
echo.
echo To deactivate, run: ggo clean
`, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.tmpDir,
		r.shareInfo.ConnectionURL)

	if err := os.WriteFile(batFile, []byte(batContent), 0644); err != nil {
		out.Error(fmt.Sprintf("Failed to write batch env file: %v", err))
		return
	}

	out.Println()
	out.Println("Temporary GPU environment set up successfully!")
	out.Println()
	out.Printf("   Connection URL: %s\n", r.shareInfo.ConnectionURL)
	out.Printf("   Hardware:       %s\n", r.shareInfo.HardwareVendor)
	out.Println()
	out.Println("To activate the environment:")
	out.Println()
	out.Println("  PowerShell:")
	out.Printf("    . %s\n\n", psFile)
	out.Println("  CMD:")
	out.Printf("    %s\n\n", batFile)
	out.Println("To clean up, run:")
	out.Println("\n   ggo clean")
	out.Println()
}

// setupLongTermEnv sets up a long-term GPU environment
func setupLongTermEnv(shareInfo *api.SharePublicInfo, outputDir string, out *tui.Output) error {
	klog.Info("Setting up long-term GPU environment...")

	if outputDir == "" {
		outputDir = paths.UserDir()
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

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
		return out.Render(&longTermEnvResultWindows{shareInfo: shareInfo, outputDir: outputDir})
	}
	return out.Render(&longTermEnvResultUnix{shareInfo: shareInfo, outputDir: outputDir})
}

// longTermEnvResultUnix implements Renderable for Unix long-term env setup
type longTermEnvResultUnix struct {
	shareInfo *api.SharePublicInfo
	outputDir string
}

func (r *longTermEnvResultUnix) RenderJSON() any {
	return map[string]any{
		"success":    true,
		"config_dir": r.outputDir,
		"share":      r.shareInfo,
	}
}

func (r *longTermEnvResultUnix) RenderTUI(out *tui.Output) {
	profileSnippet := filepath.Join(r.outputDir, "profile.sh")
	profileContent := fmt.Sprintf(`# GPU Go long-term environment
# Add this to your ~/.bashrc or ~/.zshrc:
# source %s

export GPU_GO_CONNECTION_URL="%s"
export GPU_GO_WORKER_ID="%s"
export GPU_GO_VENDOR="%s"
export GPU_GO_CONFIG_DIR="%s"
`, profileSnippet, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.outputDir)

	if err := os.WriteFile(profileSnippet, []byte(profileContent), 0644); err != nil {
		out.Error(fmt.Sprintf("Failed to write profile snippet: %v", err))
		return
	}

	out.Println()
	out.Println("Long-term GPU environment set up successfully!")
	out.Println()
	out.Printf("   Config directory: %s\n", r.outputDir)
	out.Printf("   Connection URL:   %s\n", r.shareInfo.ConnectionURL)
	out.Printf("   Hardware:         %s\n", r.shareInfo.HardwareVendor)
	out.Println()
	out.Println("To activate in all new shells, add this to your ~/.bashrc or ~/.zshrc:")
	out.Printf("\n   source %s\n\n", profileSnippet)
	out.Println("To clean up, run:")
	out.Println("\n   ggo clean --all")
	out.Println()
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
	psProfile := filepath.Join(r.outputDir, "profile.ps1")
	psContent := fmt.Sprintf(`# GPU Go long-term environment (PowerShell)
# Add this to your PowerShell profile ($PROFILE):
# . "%s"

$env:GPU_GO_CONNECTION_URL = "%s"
$env:GPU_GO_WORKER_ID = "%s"
$env:GPU_GO_VENDOR = "%s"
$env:GPU_GO_CONFIG_DIR = "%s"
`, psProfile, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.outputDir)

	if err := os.WriteFile(psProfile, []byte(psContent), 0644); err != nil {
		out.Error(fmt.Sprintf("Failed to write PowerShell profile: %v", err))
		return
	}

	batFile := filepath.Join(r.outputDir, "env.bat")
	batContent := fmt.Sprintf(`@echo off
REM GPU Go long-term environment (CMD)
setx GPU_GO_CONNECTION_URL "%s"
setx GPU_GO_WORKER_ID "%s"
setx GPU_GO_VENDOR "%s"
setx GPU_GO_CONFIG_DIR "%s"
echo Environment variables set. Please restart your terminal.
`, r.shareInfo.ConnectionURL, r.shareInfo.WorkerID, r.shareInfo.HardwareVendor, r.outputDir)

	if err := os.WriteFile(batFile, []byte(batContent), 0644); err != nil {
		out.Error(fmt.Sprintf("Failed to write batch file: %v", err))
		return
	}

	out.Println()
	out.Println("Long-term GPU environment set up successfully!")
	out.Println()
	out.Printf("   Config directory: %s\n", r.outputDir)
	out.Printf("   Connection URL:   %s\n", r.shareInfo.ConnectionURL)
	out.Printf("   Hardware:         %s\n", r.shareInfo.HardwareVendor)
	out.Println()
	out.Println("To activate in all new PowerShell sessions, add this to your profile:")
	out.Println("  Run: notepad $PROFILE")
	out.Printf("  Add: . \"%s\"\n\n", psProfile)
	out.Println("Or run the batch file to set permanent environment variables:")
	out.Printf("  %s\n\n", batFile)
	out.Println("To clean up, run:")
	out.Println("\n   ggo clean --all")
	out.Println()
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

// cleanAllEnv cleans up all GPU environments (temp directories only, not ~/.gpugo)
func cleanAllEnv(out *tui.Output) error {
	klog.Info("Cleaning up all GPU environments...")

	// Only clean temporary directories created by `ggo use`, not the ~/.gpugo config directory
	tmpDirs, _ := filepath.Glob(paths.GlobPattern("gpugo-"))
	for _, dir := range tmpDirs {
		if err := os.RemoveAll(dir); err != nil {
			klog.Warningf("Failed to remove temp directory: dir=%s error=%v", dir, err)
		} else {
			klog.V(4).Infof("Removed temp directory: dir=%s", dir)
		}
	}

	return out.Render(&cleanAllResult{})
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
		out.Println("  Remove-Item Env:GPU_GO_CONNECTION_URL, Env:GPU_GO_WORKER_ID, Env:GPU_GO_VENDOR")
	} else {
		out.Println("Start a new shell or run:")
		out.Println("  unset GPU_GO_CONNECTION_URL GPU_GO_WORKER_ID GPU_GO_VENDOR")
	}
	out.Println()
}
