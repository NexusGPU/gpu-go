package use

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var paths = platform.DefaultPaths()

var (
	serverURL string
)

// NewUseCmd creates the use command
func NewUseCmd() *cobra.Command {
	var longTerm bool
	var outputDir string

	cmd := &cobra.Command{
		Use:   "use <short-code>",
		Short: "Set up a remote GPU environment",
		Long: `Set up a temporary or long-term connection to a remote GPU worker.

This command connects to a shared GPU worker and sets up the environment
so you can use the remote GPU as if it were local.

Examples:
  # Connect to a shared GPU (temporary)
  ggo use abc123

  # Set up a long-term GPU connection
  ggo use abc123 --long-term`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shortCode := args[0]
			client := api.NewClient(api.WithBaseURL(serverURL))
			ctx := context.Background()

			// Get share info
			shareInfo, err := client.GetSharePublic(ctx, shortCode)
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to get share info")
				return err
			}

			log.Info().
				Str("worker_id", shareInfo.WorkerID).
				Str("vendor", shareInfo.HardwareVendor).
				Str("connection_url", shareInfo.ConnectionURL).
				Msg("Found GPU worker")

			// Set up the environment
			if longTerm {
				return setupLongTermEnv(shareInfo, outputDir)
			}
			return setupTemporaryEnv(shareInfo)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.Flags().BoolVar(&longTerm, "long-term", false, "Set up a long-term connection")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory for configuration files")

	return cmd
}

// NewCleanCmd creates the clean command
func NewCleanCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "clean [short-code]",
		Short: "Clean up remote GPU environment",
		Long: `Clean up temporary or long-term remote GPU environment setup.

Examples:
  # Clean up a specific connection
  ggo clean abc123

  # Clean up all GPU Go connections
  ggo clean --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				return cleanAllEnv()
			}

			if len(args) == 0 {
				return fmt.Errorf("short-code is required unless --all is specified")
			}

			shortCode := args[0]
			return cleanEnv(shortCode)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Clean up all GPU Go connections")

	return cmd
}

// setupTemporaryEnv sets up a temporary GPU environment
func setupTemporaryEnv(shareInfo *api.SharePublicInfo) error {
	log.Info().Msg("Setting up temporary GPU environment...")

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gpugo-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Write connection info
	connFile := filepath.Join(tmpDir, "connection.txt")
	if err := os.WriteFile(connFile, []byte(shareInfo.ConnectionURL), 0644); err != nil {
		return fmt.Errorf("failed to write connection file: %w", err)
	}

	if platform.IsWindows() {
		return setupTemporaryEnvWindows(shareInfo, tmpDir)
	}
	return setupTemporaryEnvUnix(shareInfo, tmpDir)
}

// setupTemporaryEnvUnix sets up temporary environment on Unix systems
func setupTemporaryEnvUnix(shareInfo *api.SharePublicInfo, tmpDir string) error {
	// Set up environment variables
	envFile := filepath.Join(tmpDir, "env.sh")
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
`, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, tmpDir,
		shareInfo.HardwareVendor, shareInfo.ConnectionURL)

	if err := os.WriteFile(envFile, []byte(envContent), 0755); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	// Create activation script
	activateScript := filepath.Join(tmpDir, "activate")
	if err := os.WriteFile(activateScript, []byte("source "+envFile), 0755); err != nil {
		return fmt.Errorf("failed to write activate script: %w", err)
	}

	fmt.Println()
	fmt.Println("Temporary GPU environment set up successfully!")
	fmt.Println()
	fmt.Printf("   Connection URL: %s\n", shareInfo.ConnectionURL)
	fmt.Printf("   Hardware:       %s\n", shareInfo.HardwareVendor)
	fmt.Println()
	fmt.Println("To activate the environment, run:")
	fmt.Printf("\n   source %s\n\n", envFile)
	fmt.Println("To clean up, run:")
	fmt.Println("\n   ggo clean")
	fmt.Println()

	return nil
}

// setupTemporaryEnvWindows sets up temporary environment on Windows
func setupTemporaryEnvWindows(shareInfo *api.SharePublicInfo, tmpDir string) error {
	// PowerShell activation script
	psFile := filepath.Join(tmpDir, "env.ps1")
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
`, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, tmpDir,
		shareInfo.ConnectionURL)

	if err := os.WriteFile(psFile, []byte(psContent), 0644); err != nil {
		return fmt.Errorf("failed to write PowerShell env file: %w", err)
	}

	// CMD batch file
	batFile := filepath.Join(tmpDir, "env.bat")
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
`, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, tmpDir,
		shareInfo.ConnectionURL)

	if err := os.WriteFile(batFile, []byte(batContent), 0644); err != nil {
		return fmt.Errorf("failed to write batch env file: %w", err)
	}

	fmt.Println()
	fmt.Println("Temporary GPU environment set up successfully!")
	fmt.Println()
	fmt.Printf("   Connection URL: %s\n", shareInfo.ConnectionURL)
	fmt.Printf("   Hardware:       %s\n", shareInfo.HardwareVendor)
	fmt.Println()
	fmt.Println("To activate the environment:")
	fmt.Println()
	fmt.Println("  PowerShell:")
	fmt.Printf("    . %s\n\n", psFile)
	fmt.Println("  CMD:")
	fmt.Printf("    %s\n\n", batFile)
	fmt.Println("To clean up, run:")
	fmt.Println("\n   ggo clean")
	fmt.Println()

	return nil
}

// setupLongTermEnv sets up a long-term GPU environment
func setupLongTermEnv(shareInfo *api.SharePublicInfo, outputDir string) error {
	log.Info().Msg("Setting up long-term GPU environment...")

	if outputDir == "" {
		outputDir = paths.UserDir()
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write connection configuration
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
		return setupLongTermEnvWindows(shareInfo, outputDir)
	}
	return setupLongTermEnvUnix(shareInfo, outputDir)
}

// setupLongTermEnvUnix sets up long-term environment on Unix
func setupLongTermEnvUnix(shareInfo *api.SharePublicInfo, outputDir string) error {
	// Write shell profile snippet
	profileSnippet := filepath.Join(outputDir, "profile.sh")
	profileContent := fmt.Sprintf(`# GPU Go long-term environment
# Add this to your ~/.bashrc or ~/.zshrc:
# source %s

export GPU_GO_CONNECTION_URL="%s"
export GPU_GO_WORKER_ID="%s"
export GPU_GO_VENDOR="%s"
export GPU_GO_CONFIG_DIR="%s"
`, profileSnippet, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, outputDir)

	if err := os.WriteFile(profileSnippet, []byte(profileContent), 0644); err != nil {
		return fmt.Errorf("failed to write profile snippet: %w", err)
	}

	fmt.Println()
	fmt.Println("Long-term GPU environment set up successfully!")
	fmt.Println()
	fmt.Printf("   Config directory: %s\n", outputDir)
	fmt.Printf("   Connection URL:   %s\n", shareInfo.ConnectionURL)
	fmt.Printf("   Hardware:         %s\n", shareInfo.HardwareVendor)
	fmt.Println()
	fmt.Println("To activate in all new shells, add this to your ~/.bashrc or ~/.zshrc:")
	fmt.Printf("\n   source %s\n\n", profileSnippet)
	fmt.Println("To clean up, run:")
	fmt.Println("\n   ggo clean --all")
	fmt.Println()

	return nil
}

// setupLongTermEnvWindows sets up long-term environment on Windows
func setupLongTermEnvWindows(shareInfo *api.SharePublicInfo, outputDir string) error {
	// Write PowerShell profile snippet
	psProfile := filepath.Join(outputDir, "profile.ps1")
	psContent := fmt.Sprintf(`# GPU Go long-term environment (PowerShell)
# Add this to your PowerShell profile ($PROFILE):
# . "%s"

$env:GPU_GO_CONNECTION_URL = "%s"
$env:GPU_GO_WORKER_ID = "%s"
$env:GPU_GO_VENDOR = "%s"
$env:GPU_GO_CONFIG_DIR = "%s"
`, psProfile, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, outputDir)

	if err := os.WriteFile(psProfile, []byte(psContent), 0644); err != nil {
		return fmt.Errorf("failed to write PowerShell profile: %w", err)
	}

	// Write CMD environment setup script
	batFile := filepath.Join(outputDir, "env.bat")
	batContent := fmt.Sprintf(`@echo off
REM GPU Go long-term environment (CMD)
setx GPU_GO_CONNECTION_URL "%s"
setx GPU_GO_WORKER_ID "%s"
setx GPU_GO_VENDOR "%s"
setx GPU_GO_CONFIG_DIR "%s"
echo Environment variables set. Please restart your terminal.
`, shareInfo.ConnectionURL, shareInfo.WorkerID, shareInfo.HardwareVendor, outputDir)

	if err := os.WriteFile(batFile, []byte(batContent), 0644); err != nil {
		return fmt.Errorf("failed to write batch file: %w", err)
	}

	fmt.Println()
	fmt.Println("Long-term GPU environment set up successfully!")
	fmt.Println()
	fmt.Printf("   Config directory: %s\n", outputDir)
	fmt.Printf("   Connection URL:   %s\n", shareInfo.ConnectionURL)
	fmt.Printf("   Hardware:         %s\n", shareInfo.HardwareVendor)
	fmt.Println()
	fmt.Println("To activate in all new PowerShell sessions, add this to your profile:")
	fmt.Println("  Run: notepad $PROFILE")
	fmt.Printf("  Add: . \"%s\"\n\n", psProfile)
	fmt.Println("Or run the batch file to set permanent environment variables:")
	fmt.Printf("  %s\n\n", batFile)
	fmt.Println("To clean up, run:")
	fmt.Println("\n   ggo clean --all")
	fmt.Println()

	return nil
}

// cleanEnv cleans up a specific GPU environment
func cleanEnv(shortCode string) error {
	log.Info().Str("short_code", shortCode).Msg("Cleaning up GPU environment...")

	// Clean temporary directories using platform-appropriate glob
	tmpDirs, _ := filepath.Glob(paths.GlobPattern("gpugo-"))
	for _, dir := range tmpDirs {
		if err := os.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("Failed to remove temp directory")
		}
	}

	fmt.Println("GPU environment cleaned up successfully!")
	return nil
}

// cleanAllEnv cleans up all GPU environments
func cleanAllEnv() error {
	log.Info().Msg("Cleaning up all GPU environments...")

	// Clean temporary directories using platform-appropriate glob
	tmpDirs, _ := filepath.Glob(paths.GlobPattern("gpugo-"))
	for _, dir := range tmpDirs {
		if err := os.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("Failed to remove temp directory")
		} else {
			log.Debug().Str("dir", dir).Msg("Removed temp directory")
		}
	}

	// Clean long-term config from user directory
	gpugoDir := paths.UserDir()
	if _, err := os.Stat(gpugoDir); err == nil {
		if err := os.RemoveAll(gpugoDir); err != nil {
			log.Warn().Err(err).Msg("Failed to remove .gpugo directory")
		} else {
			log.Debug().Str("dir", gpugoDir).Msg("Removed .gpugo directory")
		}
	}

	// Unset environment variables (remind user)
	fmt.Println("All GPU environments cleaned up successfully!")
	fmt.Println()
	fmt.Println("Note: Environment variables in your current shell are not affected.")
	if platform.IsWindows() {
		fmt.Println("Start a new shell or run in PowerShell:")
		fmt.Println("  Remove-Item Env:GPU_GO_CONNECTION_URL, Env:GPU_GO_WORKER_ID, Env:GPU_GO_VENDOR")
	} else {
		fmt.Println("Start a new shell or run:")
		fmt.Println("  unset GPU_GO_CONNECTION_URL GPU_GO_WORKER_ID GPU_GO_VENDOR")
	}
	fmt.Println()

	return nil
}
