package use

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var paths = platform.DefaultPaths()

// Windows shell type constants
const (
	shellPowerShell = "powershell"
	shellCMD        = "cmd"
)

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
// Returns nil on macOS (use command is disabled on macOS)
func NewUseCmd() *cobra.Command {
	// Disable use command on macOS
	if platform.IsDarwin() {
		return nil
	}

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

  # Activate in current shell (recommended)
  eval "$(ggo use abc123 -y)"

  # Set up a long-term GPU connection (persists across shell sessions)
  ggo use abc123 --long-term`,
		Args: cobra.ExactArgs(1),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize klog flags if not already initialized
			klog.InitFlags(nil)
			// Disable logtostderr so that stderrthreshold takes effect
			// When logtostderr=true (default), ALL logs go to stderr ignoring stderrthreshold
			flag.Set("logtostderr", "false")
			// Set stderrthreshold to WARNING level - only WARNING and ERROR will be shown
			flag.Set("stderrthreshold", "WARNING")
		},
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

			// Download required libraries first (silent when -y is used for eval)
			// Filter by vendor from share info to avoid downloading unnecessary libraries
			if err := ensureRemoteGPUClientLibs(ctx, out, shareInfo.HardwareVendor, yes); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to ensure GPU client libraries: error=%v", err)
				return fmt.Errorf("failed to download GPU client libraries: %w", err)
			}

			// Download GPU binary (like nvidia-smi) if available for this vendor
			if err := ensureGPUBinary(ctx, out, shareInfo.HardwareVendor, yes); err != nil {
				// Non-fatal: GPU binary is optional
				klog.Warningf("Failed to ensure GPU binary: %v (continuing without it)", err)
			}

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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Auto-activate environment (use with eval: eval \"$(ggo use ... -y)\")")

	return cmd
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

// isYesResponse checks if user response is affirmative (empty, "y", or "yes")
func isYesResponse(response string) bool {
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "" || response == "y" || response == "yes"
}

// ensureRemoteGPUClientLibs downloads remote-gpu-client libraries if not already present
// vendorSlug filters by vendor (e.g., "nvidia", "amd") to avoid downloading unnecessary libraries
func ensureRemoteGPUClientLibs(ctx context.Context, out *tui.Output, vendorSlug string, silent bool) error {
	depsMgr := deps.NewManager()

	// Target library types that are needed for GPU client functionality
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	if !silent && !out.IsJSON() {
		out.Printf("Downloading GPU client libraries for %s...\n", vendorSlug)
	}

	progressFn := func(lib deps.Library, downloaded, total int64) {
		if !silent && !out.IsJSON() && total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}

	libs, err := depsMgr.EnsureLibrariesByTypes(ctx, targetTypes, vendorSlug, progressFn)
	if err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	if !silent && !out.IsJSON() {
		if len(libs) > 0 {
			fmt.Println()
			out.Success("GPU client libraries downloaded successfully!")
		} else {
			klog.V(4).Info("All required GPU client libraries are already downloaded")
		}
	}

	return nil
}

// ensureGPUBinary downloads GPU binary tools (like nvidia-smi) if available for the vendor
func ensureGPUBinary(ctx context.Context, out *tui.Output, vendorSlug string, silent bool) error {
	binPath, err := deps.EnsureGPUBinary(ctx, paths, vendorSlug)
	if err != nil {
		return err
	}
	if binPath == "" {
		klog.V(2).Infof("No GPU binary available for vendor %s", vendorSlug)
		return nil
	}

	if !silent && !out.IsJSON() {
		binaryName := deps.GetGPUBinaryName(vendorSlug)
		out.Printf("GPU binary %s is available at %s\n", binaryName, binPath)
	}

	return nil
}

// getGPUBinDir returns the directory containing GPU binaries
func getGPUBinDir() string {
	return filepath.Join(paths.CacheDir(), "bin")
}

// NewCleanCmd creates the clean command
// Returns nil on macOS (clean command is disabled on macOS)
func NewCleanCmd() *cobra.Command {
	// Disable clean command on macOS
	if platform.IsDarwin() {
		return nil
	}

	var all bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "clean [short-link]",
		Short: "Clean up remote GPU environment",
		Long: `Clean up temporary or long-term remote GPU environment setup.

Examples:
  # Clean up current shell environment (if activated with ggo use)
  ggo clean

  # Clean up a specific connection (using code or link)
  ggo clean abc123
  ggo clean https://go.gpu.tf/s/abc123

  # Clean up all GPU Go connections
  ggo clean --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()

			// If -y flag, output shell commands to restore environment (for eval)
			if yes {
				return cleanEnvEval(out)
			}

			if all {
				return cleanAllEnv(out)
			}

			if len(args) == 0 {
				// Default to cleaning current shell (show instructions or prompt)
				return cleanCurrentEnv(out)
			}

			shortCode := extractShortCode(args[0])
			return cleanEnv(shortCode, out)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Clean up all GPU Go connections")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Deactivate environment non-interactively (use with eval: eval \"$(ggo clean -y)\")")

	return cmd
}

// setupTemporaryEnv sets up a temporary GPU environment
// When yes=true, outputs shell commands for eval (user runs: eval "$(ggo use xxx -y)")
func setupTemporaryEnv(shareInfo *api.SharePublicInfo, yes bool, out *tui.Output) error {
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
		return renderWindowsEnv(shareInfo, config, envResult, yes, out)
	}
	return renderUnixEnv(shareInfo, config, envResult, yes, out)
}

// renderUnixEnv renders and optionally activates the Unix environment
// When yes=true, outputs shell commands for eval (designed to be run via: eval "$(ggo use xxx -y)")
func renderUnixEnv(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, yes bool, out *tui.Output) error {
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

	// Generate and write clean script
	cleanScript := generateCleanScript()
	cleanFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "clean.sh")
	if err := os.WriteFile(cleanFile, []byte(cleanScript), 0755); err != nil {
		klog.Warningf("Failed to write clean script: %v", err)
	}

	// If -y flag, output shell commands for eval
	if yes {
		return outputEvalCommands(config, envResult, envFile, cleanFile, out)
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

	// Prompt user
	if !out.IsJSON() {
		out.Println(styles.Subtitle.Render("Activate Environment"))
		out.Println()
		out.Printf("Would you like to activate the GPU environment in a new shell? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		shouldActivate := isYesResponse(response)

		if shouldActivate {
			out.Println()
			out.Println(styles.Info.Render("Launching new shell with GPU environment..."))
			out.Println()

			// Launch a new interactive shell with the environment set
			if err := launchGPUShell(config, envResult, envFile); err != nil {
				klog.Warningf("Failed to launch GPU shell: %v", err)
				out.Warning("Failed to launch shell automatically.")
				out.Println()
				out.Println("You can manually activate by running:")
				out.Printf("\n   source %s\n\n", envFile)
				return nil
			}

			// After shell exits, show message
			out.Println()
			out.Println(styles.Muted.Render("GPU shell session ended. Environment deactivated."))
			out.Println()
		} else {
			out.Println()
			out.Println(styles.Subtitle.Render("Manual Activation"))
			out.Println()
			out.Println("You can activate later by running:")
			out.Printf("\n   source %s\n\n", envFile)
		}
	}

	return out.Render(&tempEnvResultUnix{shareInfo: shareInfo, envFile: envFile, envResult: envResult})
}

// launchGPUShell launches a new interactive shell with the GPU environment variables set
func launchGPUShell(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile string) error {
	// Determine the user's shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	// LibsPath is for .so files (used for LD_LIBRARY_PATH, LD_PRELOAD)
	libsPath := config.LibsPath
	if libsPath == "" {
		libsPath = paths.LibsDir()
	}

	// BinDir is for GPU binaries like nvidia-smi
	binDir := getGPUBinDir()

	// Create the command
	cmd := exec.Command(shell, "-i")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Copy current environment and add GPU environment variables
	env := os.Environ()

	// Add TensorFusion environment variables
	for k, v := range envResult.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build LD_LIBRARY_PATH - use libs directory (contains only .so files)
	existingLDPath := os.Getenv("LD_LIBRARY_PATH")
	if existingLDPath != "" {
		env = append(env, fmt.Sprintf("LD_LIBRARY_PATH=%s:%s", libsPath, existingLDPath))
	} else {
		env = append(env, fmt.Sprintf("LD_LIBRARY_PATH=%s", libsPath))
	}

	// Add GPU bin directory to PATH (for nvidia-smi, amdsmi, etc.)
	existingPath := os.Getenv("PATH")
	if existingPath != "" {
		env = append(env, fmt.Sprintf("PATH=%s:%s", binDir, existingPath))
	} else {
		env = append(env, fmt.Sprintf("PATH=%s", binDir))
	}

	// Build LD_PRELOAD - use libs directory
	libNames := studio.GetLibraryNames(config.Vendor)
	if len(libNames) > 0 {
		var preloadPaths []string
		for _, lib := range libNames {
			preloadPaths = append(preloadPaths, filepath.Join(libsPath, lib))
		}
		existingPreload := os.Getenv("LD_PRELOAD")
		if existingPreload != "" {
			env = append(env, fmt.Sprintf("LD_PRELOAD=%s:%s", strings.Join(preloadPaths, ":"), existingPreload))
		} else {
			env = append(env, fmt.Sprintf("LD_PRELOAD=%s", strings.Join(preloadPaths, ":")))
		}
	}

	// Mark as GPU Go activated
	env = append(env, "_GGO_ACTIVE=1")
	env = append(env, fmt.Sprintf("_GGO_LIBS_PATH=%s", libsPath))
	env = append(env, fmt.Sprintf("_GGO_BIN_PATH=%s", binDir))

	cmd.Env = env

	// Print GPU environment banner
	fmt.Printf("\n%s GPU environment activated %s\n", styles().Success.Render("✓"), styles().Muted.Render("(type 'exit' to deactivate)"))
	fmt.Println()

	// Run the shell and wait for it to exit
	return cmd.Run()
}

// styles returns the default TUI styles
func styles() *tui.Styles {
	return tui.DefaultStyles()
}

// outputEvalCommands outputs shell commands for eval mode
func outputEvalCommands(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile, cleanFile string, out *tui.Output) error {
	if platform.IsWindows() {
		return outputEvalCommandsWindows(config, envResult, envFile, cleanFile, out)
	}
	return outputEvalCommandsUnix(config, envResult, envFile, cleanFile, out)
}

// outputEvalCommandsUnix outputs shell commands for eval mode (Unix/Linux)
func outputEvalCommandsUnix(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile, cleanFile string, out *tui.Output) error {
	// LibsPath is for .so files (used for LD_LIBRARY_PATH, LD_PRELOAD)
	libsPath := config.LibsPath
	if libsPath == "" {
		libsPath = paths.LibsDir()
	}

	// BinDir is for GPU binaries like nvidia-smi
	binDir := getGPUBinDir()

	var script strings.Builder

	// Save original values for later restoration
	script.WriteString("# Save original environment for cleanup\n")
	script.WriteString("export _GGO_ORIG_LD_LIBRARY_PATH=\"$LD_LIBRARY_PATH\"\n")
	script.WriteString("export _GGO_ORIG_LD_PRELOAD=\"$LD_PRELOAD\"\n")
	script.WriteString("export _GGO_ORIG_PATH=\"$PATH\"\n")
	script.WriteString(fmt.Sprintf("export _GGO_CLEAN_FILE=\"%s\"\n", cleanFile))
	script.WriteString("\n")

	// Export TensorFusion environment variables
	for k, v := range envResult.EnvVars {
		script.WriteString(fmt.Sprintf("export %s=\"%s\"\n", k, v))
	}

	// Add LD_LIBRARY_PATH - use libs directory (contains only .so files)
	script.WriteString(fmt.Sprintf("export LD_LIBRARY_PATH=\"%s${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}\"\n", libsPath))

	// Add GPU bin directory to PATH (for nvidia-smi, amdsmi, etc.)
	script.WriteString(fmt.Sprintf("export PATH=\"%s${PATH:+:$PATH}\"\n", binDir))

	// Add LD_PRELOAD based on vendor - use libs directory
	libNames := studio.GetLibraryNames(config.Vendor)
	if len(libNames) > 0 {
		var preloadPaths []string
		for _, lib := range libNames {
			preloadPaths = append(preloadPaths, filepath.Join(libsPath, lib))
		}
		script.WriteString(fmt.Sprintf("export LD_PRELOAD=\"%s${LD_PRELOAD:+:$LD_PRELOAD}\"\n", strings.Join(preloadPaths, ":")))
	}

	// Mark as activated
	script.WriteString("export _GGO_ACTIVE=1\n")
	script.WriteString(fmt.Sprintf("export _GGO_LIBS_PATH=\"%s\"\n", libsPath))
	script.WriteString(fmt.Sprintf("export _GGO_BIN_PATH=\"%s\"\n", binDir))
	script.WriteString("\n")

	// Define ggo wrapper function to handle clean command automatically
	script.WriteString("# Define ggo wrapper function for automatic clean handling\n")
	script.WriteString("_ggo_real=$(command -v ggo)\n")
	script.WriteString("ggo() {\n")
	script.WriteString("  if [ \"$1\" = \"clean\" ] && [ -z \"$2\" ]; then\n")
	script.WriteString("    eval \"$($_ggo_real clean -y)\"\n")
	script.WriteString("  else\n")
	script.WriteString("    $_ggo_real \"$@\"\n")
	script.WriteString("  fi\n")
	script.WriteString("}\n")
	script.WriteString("\n")

	// Print activation message to stderr so it doesn't interfere with eval
	script.WriteString("echo \"GPU Go environment activated for vendor: " + string(config.Vendor) + "\" >&2\n")
	script.WriteString("echo \"Connection URL: " + config.ConnectionURL + "\" >&2\n")
	script.WriteString("echo \"\" >&2\n")
	script.WriteString("echo \"To deactivate and restore your environment, run:\" >&2\n")
	script.WriteString("echo '  ggo clean' >&2\n")

	// Output to stdout for eval
	fmt.Print(script.String())
	return nil
}

// outputEvalCommandsWindows outputs shell commands for eval mode (Windows)
// Detects PowerShell vs CMD and outputs appropriate commands
func outputEvalCommandsWindows(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile, cleanFile string, out *tui.Output) error {
	// LibsPath is for .dll files (used for PATH on Windows)
	libsPath := config.LibsPath
	if libsPath == "" {
		libsPath = paths.LibsDir()
	}

	// Detect shell type by checking COMSPEC and PSModulePath
	// PowerShell sets PSModulePath, CMD doesn't
	shell := detectWindowsShell()

	if shell == shellPowerShell {
		return outputEvalCommandsPowerShell(config, envResult, envFile, cleanFile, libsPath, out)
	}
	return outputEvalCommandsCMD(config, envResult, envFile, cleanFile, libsPath, out)
}

// detectWindowsShell detects whether we're in PowerShell or CMD
// Returns shellPowerShell for Windows PowerShell and PowerShell Core (pwsh)
// Returns shellCMD for Command Prompt
func detectWindowsShell() string {
	// Method 1: Check PSModulePath - always set in PowerShell sessions
	if os.Getenv("PSModulePath") != "" {
		return shellPowerShell
	}

	// Method 2: Check if running under Windows Terminal with PowerShell
	// WT_SESSION is set by Windows Terminal
	if os.Getenv("WT_SESSION") != "" {
		// Check for PowerShell-specific markers
		if os.Getenv("PSVersionTable") != "" {
			return shellPowerShell
		}
	}

	// Method 3: Check SHELL env (sometimes set by Git Bash or other tools)
	shell := os.Getenv("SHELL")
	if strings.Contains(strings.ToLower(shell), shellPowerShell) || strings.Contains(strings.ToLower(shell), "pwsh") {
		return shellPowerShell
	}

	// Default to CMD - safer choice as CMD users can be explicitly guided
	return shellCMD
}

// escapeForPowerShell escapes a string for safe use in PowerShell double-quoted strings
func escapeForPowerShell(s string) string {
	s = strings.ReplaceAll(s, "`", "``")   // Escape backticks first
	s = strings.ReplaceAll(s, "$", "`$")   // Escape dollar signs
	s = strings.ReplaceAll(s, "\"", "`\"") // Escape double quotes
	return s
}

// outputEvalCommandsPowerShell outputs PowerShell commands for eval mode
func outputEvalCommandsPowerShell(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile, cleanFile, libsPath string, out *tui.Output) error {
	// BinDir is for GPU binaries like nvidia-smi
	binDir := getGPUBinDir()

	var script strings.Builder

	// Save original values for later restoration
	script.WriteString("# Save original environment for cleanup\n")
	script.WriteString("$env:_GGO_ORIG_PATH = $env:PATH\n")
	script.WriteString(fmt.Sprintf("$env:_GGO_CLEAN_FILE = \"%s\"\n", escapeForPowerShell(cleanFile)))
	script.WriteString("\n")

	// Export TensorFusion environment variables
	for k, v := range envResult.EnvVars {
		script.WriteString(fmt.Sprintf("$env:%s = \"%s\"\n", k, escapeForPowerShell(v)))
	}

	// Set GPU vendor
	script.WriteString(fmt.Sprintf("$env:TF_GPU_VENDOR = \"%s\"\n", config.Vendor))

	// Add libs path and bin path to PATH at the front
	script.WriteString(fmt.Sprintf("$env:PATH = \"%s;%s;\" + $env:PATH\n", escapeForPowerShell(binDir), escapeForPowerShell(libsPath)))

	// Set CUDA_PATH - point to libs directory
	script.WriteString(fmt.Sprintf("$env:CUDA_PATH = \"%s\"\n", escapeForPowerShell(libsPath)))
	script.WriteString(fmt.Sprintf("$env:CUDA_HOME = \"%s\"\n", escapeForPowerShell(libsPath)))

	// Mark as activated
	script.WriteString("$env:_GGO_ACTIVE = \"1\"\n")
	script.WriteString(fmt.Sprintf("$env:_GGO_LIBS_PATH = \"%s\"\n", escapeForPowerShell(libsPath)))
	script.WriteString(fmt.Sprintf("$env:_GGO_BIN_PATH = \"%s\"\n", escapeForPowerShell(binDir)))
	script.WriteString("\n")

	// Define ggo wrapper function for automatic clean handling
	// Note: We use $MyArgs instead of $args since $args is an automatic variable
	script.WriteString("# Define ggo wrapper function for automatic clean handling\n")
	script.WriteString("$Global:_ggo_real = (Get-Command ggo -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source\n")
	script.WriteString("if (-not $Global:_ggo_real) { $Global:_ggo_real = \"ggo\" }\n")
	script.WriteString("function Global:ggo {\n")
	script.WriteString("  if ($args.Count -eq 1 -and $args[0] -eq \"clean\") {\n")
	script.WriteString("    & $Global:_ggo_real clean -y | Out-String | Invoke-Expression\n")
	script.WriteString("  } else {\n")
	script.WriteString("    & $Global:_ggo_real @args\n")
	script.WriteString("  }\n")
	script.WriteString("}\n")
	script.WriteString("\n")

	// Print activation message to stderr using [Console]::Error
	script.WriteString("[Console]::Error.WriteLine(\"GPU Go environment activated for vendor: " + string(config.Vendor) + "\")\n")
	script.WriteString("[Console]::Error.WriteLine(\"Connection URL: " + escapeForPowerShell(config.ConnectionURL) + "\")\n")
	script.WriteString("[Console]::Error.WriteLine(\"\")\n")
	script.WriteString("[Console]::Error.WriteLine(\"To deactivate and restore your environment, run:\")\n")
	script.WriteString("[Console]::Error.WriteLine(\"  ggo clean\")\n")

	// Output to stdout for eval
	fmt.Print(script.String())
	return nil
}

// outputEvalCommandsCMD outputs CMD batch commands for eval mode
// Note: CMD doesn't support true eval like bash/PowerShell. Instead, we output
// a message guiding users to use the generated batch file directly.
func outputEvalCommandsCMD(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, envFile, cleanFile, libsPath string, out *tui.Output) error {
	// CMD doesn't support eval-style command execution like bash or PowerShell
	// The best we can do is tell users to call the batch file directly
	// Output message to stderr
	fmt.Fprintf(os.Stderr, "CMD does not support eval-style activation.\n")
	fmt.Fprintf(os.Stderr, "Please run the following command to activate GPU environment:\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "  call \"%s\"\n", envFile)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Or use PowerShell for better experience:\n")
	fmt.Fprintf(os.Stderr, "  powershell -Command \"ggo use %s -y | Out-String | Invoke-Expression\"\n", extractShortCode(config.ConnectionURL))
	fmt.Fprintf(os.Stderr, "\n")

	// Output the call command to stdout so user can copy-paste
	fmt.Printf("call \"%s\"\n", envFile)
	return nil
}

// generateCleanScript generates a shell script to clean up the GPU environment
func generateCleanScript() string {
	if platform.IsWindows() {
		return generateCleanScriptWindows()
	}
	return generateCleanScriptUnix()
}

// generateCleanScriptUnix generates a Unix shell script to clean up the GPU environment
func generateCleanScriptUnix() string {
	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("# GPU Go environment cleanup script\n")
	script.WriteString("# Generated by ggo use\n\n")

	// Restore original LD_LIBRARY_PATH (remove only TF entries)
	script.WriteString("# Restore LD_LIBRARY_PATH\n")
	script.WriteString("if [ -n \"$_GGO_ORIG_LD_LIBRARY_PATH\" ]; then\n")
	script.WriteString("  export LD_LIBRARY_PATH=\"$_GGO_ORIG_LD_LIBRARY_PATH\"\n")
	script.WriteString("else\n")
	script.WriteString("  unset LD_LIBRARY_PATH\n")
	script.WriteString("fi\n\n")

	// Restore original LD_PRELOAD (remove only TF entries)
	script.WriteString("# Restore LD_PRELOAD\n")
	script.WriteString("if [ -n \"$_GGO_ORIG_LD_PRELOAD\" ]; then\n")
	script.WriteString("  export LD_PRELOAD=\"$_GGO_ORIG_LD_PRELOAD\"\n")
	script.WriteString("else\n")
	script.WriteString("  unset LD_PRELOAD\n")
	script.WriteString("fi\n\n")

	// Restore original PATH
	script.WriteString("# Restore PATH\n")
	script.WriteString("if [ -n \"$_GGO_ORIG_PATH\" ]; then\n")
	script.WriteString("  export PATH=\"$_GGO_ORIG_PATH\"\n")
	script.WriteString("fi\n\n")

	// Unset TensorFusion environment variables
	script.WriteString("# Unset TensorFusion environment variables\n")
	script.WriteString("unset TENSOR_FUSION_OPERATOR_CONNECTION_INFO\n")
	script.WriteString("unset TF_LOG_PATH\n")
	script.WriteString("unset TF_LOG_LEVEL\n")
	script.WriteString("unset TF_ENABLE_LOG\n")
	script.WriteString("unset TF_GPU_VENDOR\n\n")

	// Unset internal tracking variables
	script.WriteString("# Unset internal tracking variables\n")
	script.WriteString("unset _GGO_ORIG_LD_LIBRARY_PATH\n")
	script.WriteString("unset _GGO_ORIG_LD_PRELOAD\n")
	script.WriteString("unset _GGO_ORIG_PATH\n")
	script.WriteString("unset _GGO_ACTIVE\n")
	script.WriteString("unset _GGO_LIBS_PATH\n")
	script.WriteString("unset _GGO_CLEAN_FILE\n\n")

	// Remove ggo wrapper function
	script.WriteString("# Remove ggo wrapper function\n")
	script.WriteString("unset -f ggo 2>/dev/null\n")
	script.WriteString("unset _ggo_real\n\n")

	script.WriteString("echo \"GPU Go environment deactivated\"\n")

	return script.String()
}

// generateCleanScriptWindows generates PowerShell cleanup script
func generateCleanScriptWindows() string {
	var script strings.Builder
	script.WriteString("# GPU Go environment cleanup script (PowerShell)\n")
	script.WriteString("# Generated by ggo use\n")
	script.WriteString("# Usage: . clean.ps1\n\n")

	// Check if environment is active
	script.WriteString("if (-not $env:_GGO_ACTIVE) {\n")
	script.WriteString("  Write-Host 'GPU Go environment is not active' -ForegroundColor Yellow\n")
	script.WriteString("  return\n")
	script.WriteString("}\n\n")

	// Restore original PATH
	script.WriteString("# Restore PATH\n")
	script.WriteString("if ($env:_GGO_ORIG_PATH) {\n")
	script.WriteString("  $env:PATH = $env:_GGO_ORIG_PATH\n")
	script.WriteString("}\n\n")

	// Unset TensorFusion environment variables
	script.WriteString("# Unset TensorFusion environment variables\n")
	script.WriteString("Remove-Item Env:TENSOR_FUSION_OPERATOR_CONNECTION_INFO -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:TF_LOG_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:TF_LOG_LEVEL -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:TF_ENABLE_LOG -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:TF_GPU_VENDOR -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:CUDA_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:CUDA_HOME -ErrorAction SilentlyContinue\n\n")

	// Unset internal tracking variables
	script.WriteString("# Unset internal tracking variables\n")
	script.WriteString("Remove-Item Env:_GGO_ORIG_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:_GGO_ACTIVE -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:_GGO_LIBS_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Item Env:_GGO_CLEAN_FILE -ErrorAction SilentlyContinue\n\n")

	// Remove ggo wrapper function (Global scope)
	script.WriteString("# Remove ggo wrapper function\n")
	script.WriteString("Remove-Item Function:ggo -ErrorAction SilentlyContinue\n")
	script.WriteString("Remove-Variable _ggo_real -Scope Global -ErrorAction SilentlyContinue\n\n")

	script.WriteString("Write-Host 'GPU Go environment deactivated' -ForegroundColor Green\n")

	return script.String()
}

// renderWindowsEnv renders and optionally activates the Windows environment
// When yes=true, outputs shell commands for eval (designed to be run via: eval "$(ggo use xxx -y)" in PowerShell or CMD)
func renderWindowsEnv(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, yes bool, out *tui.Output) error {
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

	// Generate and write clean scripts
	cleanPSScript := generateCleanScriptWindows()
	cleanPSFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "clean.ps1")
	if err := os.WriteFile(cleanPSFile, []byte(cleanPSScript), 0644); err != nil {
		klog.Warningf("Failed to write PowerShell clean script: %v", err)
	}

	// Generate CMD clean script
	cleanBatScript := generateCleanScriptCMD()
	cleanBatFile := filepath.Join(paths.StudioConfigDir(config.StudioName), "clean.bat")
	if err := os.WriteFile(cleanBatFile, []byte(cleanBatScript), 0644); err != nil {
		klog.Warningf("Failed to write CMD clean script: %v", err)
	}

	// If -y flag, output shell commands for eval
	if yes {
		return outputEvalCommands(config, envResult, psFile, cleanPSFile, out)
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

	// Prompt user
	if !out.IsJSON() {
		out.Println(styles.Subtitle.Render("Activate Environment"))
		out.Println()
		out.Printf("Would you like to activate the GPU environment in a new shell? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		shouldActivate := isYesResponse(response)

		if shouldActivate {
			out.Println()
			out.Println(styles.Info.Render("Launching new shell with GPU environment..."))
			out.Println()

			// Launch a new interactive shell with the environment set
			if err := launchGPUShellWindows(config, envResult, psFile, batFile); err != nil {
				klog.Warningf("Failed to launch GPU shell: %v", err)
				out.Warning("Failed to launch shell automatically.")
				out.Println()
				out.Println("You can manually activate by running:")
				shell := detectWindowsShell()
				if shell == shellPowerShell {
					out.Printf("\n   . %s\n\n", psFile)
				} else {
					out.Printf("\n   %s\n\n", batFile)
				}
				return nil
			}

			// After shell exits, show message
			out.Println()
			out.Println(styles.Muted.Render("GPU shell session ended. Environment deactivated."))
			out.Println()
		} else {
			out.Println()
			out.Println(styles.Subtitle.Render("Manual Activation"))
			out.Println()
			out.Println("You can activate later by running:")
			shell := detectWindowsShell()
			if shell == shellPowerShell {
				out.Printf("\n   . %s\n\n", psFile)
			} else {
				out.Printf("\n   %s\n\n", batFile)
			}
			out.Println("Or use eval mode (recommended):")
			out.Println("\n   PowerShell: ggo use " + extractShortCode(shareInfo.WorkerID) + " -y | Out-String | Invoke-Expression")
			out.Println("   CMD:        for /f \"delims=\" %i in ('ggo use " + extractShortCode(shareInfo.WorkerID) + " -y') do @%i")
			out.Println()
		}
	}

	return out.Render(&tempEnvResultWindows{shareInfo: shareInfo, psFile: psFile, batFile: batFile, envResult: envResult})
}

// generateCleanScriptCMD generates a CMD batch script to clean up the GPU environment
func generateCleanScriptCMD() string {
	var script strings.Builder
	script.WriteString("@echo off\n")
	script.WriteString("REM GPU Go environment cleanup script (CMD)\n")
	script.WriteString("REM Generated by ggo use\n")
	script.WriteString("REM Usage: call clean.bat\n\n")

	// Check if environment is active
	script.WriteString("if not defined _GGO_ACTIVE (\n")
	script.WriteString("  echo GPU Go environment is not active\n")
	script.WriteString("  goto :eof\n")
	script.WriteString(")\n\n")

	// Restore original PATH
	script.WriteString("REM Restore PATH\n")
	script.WriteString("if defined _GGO_ORIG_PATH (\n")
	script.WriteString("  set \"PATH=%_GGO_ORIG_PATH%\"\n")
	script.WriteString(")\n\n")

	// Unset TensorFusion environment variables
	script.WriteString("REM Unset TensorFusion environment variables\n")
	script.WriteString("set \"TENSOR_FUSION_OPERATOR_CONNECTION_INFO=\"\n")
	script.WriteString("set \"TF_LOG_PATH=\"\n")
	script.WriteString("set \"TF_LOG_LEVEL=\"\n")
	script.WriteString("set \"TF_ENABLE_LOG=\"\n")
	script.WriteString("set \"TF_GPU_VENDOR=\"\n")
	script.WriteString("set \"CUDA_PATH=\"\n")
	script.WriteString("set \"CUDA_HOME=\"\n\n")

	// Unset internal tracking variables
	script.WriteString("REM Unset internal tracking variables\n")
	script.WriteString("set \"_GGO_ORIG_PATH=\"\n")
	script.WriteString("set \"_GGO_ACTIVE=\"\n")
	script.WriteString("set \"_GGO_LIBS_PATH=\"\n")
	script.WriteString("set \"_GGO_CLEAN_FILE=\"\n\n")

	script.WriteString("echo GPU Go environment deactivated\n")

	return script.String()
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

// launchGPUShellWindows launches a new interactive shell with the GPU environment variables set (Windows)
func launchGPUShellWindows(config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, psFile, batFile string) error {
	shell := detectWindowsShell()
	// LibsPath is for .dll files (used for PATH on Windows)
	libsPath := config.LibsPath
	if libsPath == "" {
		libsPath = paths.LibsDir()
	}

	var cmd *exec.Cmd
	if shell == shellPowerShell {
		// Launch PowerShell with the environment script
		cmd = exec.Command(shellPowerShell, "-NoExit", "-Command", fmt.Sprintf(". '%s'", psFile))
	} else {
		// Launch CMD with the environment script
		cmd = exec.Command(shellCMD, "/k", batFile)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Copy current environment and add GPU environment variables
	env := os.Environ()

	// Add TensorFusion environment variables
	for k, v := range envResult.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Add libs path to PATH - libs directory contains only .dll files
	existingPath := os.Getenv("PATH")
	if existingPath != "" {
		env = append(env, fmt.Sprintf("PATH=%s;%s", libsPath, existingPath))
	} else {
		env = append(env, fmt.Sprintf("PATH=%s", libsPath))
	}

	// Set CUDA_PATH - point to libs directory
	env = append(env, fmt.Sprintf("CUDA_PATH=%s", libsPath))
	env = append(env, fmt.Sprintf("CUDA_HOME=%s", libsPath))

	// Set GPU vendor
	env = append(env, fmt.Sprintf("TF_GPU_VENDOR=%s", config.Vendor))

	// Mark as GPU Go activated
	env = append(env, "_GGO_ACTIVE=1")
	env = append(env, fmt.Sprintf("_GGO_LIBS_PATH=%s", libsPath))

	cmd.Env = env

	// Print GPU environment banner
	styles := tui.DefaultStyles()
	fmt.Printf("\n%s GPU environment activated %s\n", styles.Success.Render("✓"), styles.Muted.Render("(type 'exit' to deactivate)"))
	fmt.Println()

	// Run the shell and wait for it to exit
	return cmd.Run()
}

// tempEnvResultWindows implements Renderable for Windows temporary env setup
type tempEnvResultWindows struct {
	shareInfo *api.SharePublicInfo
	psFile    string
	batFile   string
	envResult *studio.GPUEnvResult
}

func (r *tempEnvResultWindows) RenderJSON() any {
	result := map[string]any{
		"success":         true,
		"powershell_file": r.psFile,
		"batch_file":      r.batFile,
		"share":           r.shareInfo,
	}
	if r.envResult != nil {
		result["env_vars"] = r.envResult.EnvVars
	}
	return result
}

func (r *tempEnvResultWindows) RenderTUI(out *tui.Output) {
	// TUI output is handled in renderWindowsEnv
}

// setupLongTermEnv sets up a long-term GPU environment
// When yes=true, outputs shell commands for eval (user runs: eval "$(ggo use xxx -y --long-term)")
func setupLongTermEnv(shareInfo *api.SharePublicInfo, outputDir string, yes bool, out *tui.Output) error {
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
		return setupLongTermWindows(shareInfo, config, envResult, outputDir, yes, out)
	}
	return setupLongTermUnix(shareInfo, config, envResult, outputDir, yes, out)
}

// setupLongTermUnix sets up long-term environment for Unix
// When yes=true, outputs shell commands for eval
func setupLongTermUnix(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, outputDir string, yes bool, out *tui.Output) error {
	// Generate the profile script
	profileScript, err := studio.GenerateEnvScript(config, paths)
	if err != nil {
		return fmt.Errorf("failed to generate profile script: %w", err)
	}

	profileSnippet := filepath.Join(outputDir, "profile.sh")
	if err := os.WriteFile(profileSnippet, []byte(profileScript), 0755); err != nil {
		return fmt.Errorf("failed to write profile snippet: %w", err)
	}

	// Generate clean script
	cleanScript := generateCleanScript()
	cleanFile := filepath.Join(outputDir, "clean.sh")
	if err := os.WriteFile(cleanFile, []byte(cleanScript), 0755); err != nil {
		klog.Warningf("Failed to write clean script: %v", err)
	}

	// If -y flag, output shell commands for eval
	if yes {
		return outputEvalCommands(config, envResult, profileSnippet, cleanFile, out)
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
		out.Println(styles.Subtitle.Render("Permanent Activation"))
		out.Println()
		out.Printf("Add GPU environment to %s for all new shells? [Y/n]: ", filepath.Base(shellRC))

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		shouldAdd := isYesResponse(response)

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

		out.Println()
		out.Println(styles.Subtitle.Render("Current Shell Activation"))
		out.Println()
		out.Println("To activate in your current shell now:")
		out.Printf("\n   eval \"$(ggo use %s -y)\"\n\n", extractShortCode(shareInfo.WorkerID))
		out.Println("To deactivate later:")
		out.Println("\n   ggo clean")
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
// When yes=true, outputs shell commands for eval
func setupLongTermWindows(shareInfo *api.SharePublicInfo, config *studio.GPUEnvConfig, envResult *studio.GPUEnvResult, outputDir string, yes bool, out *tui.Output) error {
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
		// Skip PATH for setx as it can cause path truncation or duplication
		if k == "PATH" {
			continue
		}
		batContent.WriteString(fmt.Sprintf("setx %s \"%s\"\n", k, v))
	}
	batContent.WriteString("\necho Environment variables set. Please restart your terminal.\n")

	batFile := filepath.Join(outputDir, "setenv.bat")
	if err := os.WriteFile(batFile, []byte(batContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write batch file: %w", err)
	}

	// Generate clean scripts
	cleanPSScript := generateCleanScriptWindows()
	cleanPSFile := filepath.Join(outputDir, "clean.ps1")
	if err := os.WriteFile(cleanPSFile, []byte(cleanPSScript), 0644); err != nil {
		klog.Warningf("Failed to write PowerShell clean script: %v", err)
	}

	cleanBatScript := generateCleanScriptCMD()
	cleanBatFile := filepath.Join(outputDir, "clean.bat")
	if err := os.WriteFile(cleanBatFile, []byte(cleanBatScript), 0644); err != nil {
		klog.Warningf("Failed to write CMD clean script: %v", err)
	}

	// If -y flag, output shell commands for eval
	if yes {
		return outputEvalCommands(config, envResult, psProfilePath, cleanPSFile, out)
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
	shell := detectWindowsShell()
	if shell == shellPowerShell && !out.IsJSON() {
		out.Println(styles.Subtitle.Render("Permanent Activation"))
		out.Println()
		out.Printf("Add GPU environment to PowerShell profile for all new shells? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		shouldAdd := isYesResponse(response)

		if shouldAdd {
			profilePath := os.Getenv("PROFILE")
			if profilePath == "" {
				// Default PowerShell profile path
				profilePath = filepath.Join(os.Getenv("USERPROFILE"), "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
			}

			sourceLine := fmt.Sprintf("\n# GPU Go environment\n. \"%s\"\n", psProfilePath)
			if err := appendToFile(profilePath, sourceLine, psProfilePath); err != nil {
				out.Warning(fmt.Sprintf("Failed to update PowerShell profile: %v", err))
			} else {
				out.Success(fmt.Sprintf("Added to PowerShell profile: %s", profilePath))
				out.Println()
				out.Println("Restart your terminal or run:")
				out.Printf("\n   . $PROFILE\n\n")
			}
		}

		out.Println()
		out.Println(styles.Subtitle.Render("Current Shell Activation"))
		out.Println()
		out.Println("To activate in your current shell now:")
		out.Printf("\n   ggo use %s -y | Out-String | Invoke-Expression\n\n", extractShortCode(shareInfo.WorkerID))
		out.Println("To deactivate later:")
		out.Println("\n   ggo clean")
		out.Println()
	} else if !out.IsJSON() {
		out.Println(styles.Subtitle.Render("Permanent Activation"))
		out.Println()
		out.Println("To activate in all new PowerShell sessions, add to your profile:")
		out.Println("  Run: notepad $PROFILE")
		out.Printf("  Add: . \"%s\"\n\n", psProfilePath)
		out.Println("Or set permanent environment variables:")
		out.Printf("  %s\n\n", batFile)
		out.Println("To activate in current CMD session:")
		out.Printf("\n   for /f \"delims=\" %%i in ('ggo use %s -y') do @%%i\n\n", extractShortCode(shareInfo.WorkerID))
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

// cleanEnvEval outputs shell commands to restore environment for eval mode
func cleanEnvEval(out *tui.Output) error {
	if platform.IsWindows() {
		return cleanEnvEvalWindows(out)
	}
	return cleanEnvEvalUnix(out)
}

// cleanEnvEvalUnix outputs shell commands to restore environment for eval mode (Unix/Linux)
func cleanEnvEvalUnix(out *tui.Output) error {
	var script strings.Builder

	// Check if environment is active
	script.WriteString("if [ -z \"$_GGO_ACTIVE\" ]; then\n")
	script.WriteString("  echo 'GPU Go environment is not active' >&2\n")
	script.WriteString("else\n")

	// Restore original LD_LIBRARY_PATH
	script.WriteString("  if [ -n \"$_GGO_ORIG_LD_LIBRARY_PATH\" ]; then\n")
	script.WriteString("    export LD_LIBRARY_PATH=\"$_GGO_ORIG_LD_LIBRARY_PATH\"\n")
	script.WriteString("  else\n")
	script.WriteString("    unset LD_LIBRARY_PATH\n")
	script.WriteString("  fi\n")

	// Restore original LD_PRELOAD
	script.WriteString("  if [ -n \"$_GGO_ORIG_LD_PRELOAD\" ]; then\n")
	script.WriteString("    export LD_PRELOAD=\"$_GGO_ORIG_LD_PRELOAD\"\n")
	script.WriteString("  else\n")
	script.WriteString("    unset LD_PRELOAD\n")
	script.WriteString("  fi\n")

	// Restore original PATH
	script.WriteString("  if [ -n \"$_GGO_ORIG_PATH\" ]; then\n")
	script.WriteString("    export PATH=\"$_GGO_ORIG_PATH\"\n")
	script.WriteString("  fi\n")

	// Unset TensorFusion environment variables
	script.WriteString("  unset TENSOR_FUSION_OPERATOR_CONNECTION_INFO\n")
	script.WriteString("  unset TF_LOG_PATH\n")
	script.WriteString("  unset TF_LOG_LEVEL\n")
	script.WriteString("  unset TF_ENABLE_LOG\n")
	script.WriteString("  unset TF_GPU_VENDOR\n")

	// Unset internal tracking variables
	script.WriteString("  unset _GGO_ORIG_LD_LIBRARY_PATH\n")
	script.WriteString("  unset _GGO_ORIG_LD_PRELOAD\n")
	script.WriteString("  unset _GGO_ORIG_PATH\n")
	script.WriteString("  unset _GGO_ACTIVE\n")
	script.WriteString("  unset _GGO_LIBS_PATH\n")
	script.WriteString("  unset _GGO_CLEAN_FILE\n")

	// Remove ggo wrapper function
	script.WriteString("  unset -f ggo 2>/dev/null\n")
	script.WriteString("  unset _ggo_real\n")

	script.WriteString("  echo 'GPU Go environment deactivated' >&2\n")
	script.WriteString("fi\n")

	// Output to stdout for eval
	fmt.Print(script.String())
	return nil
}

// cleanEnvEvalWindows outputs shell commands to restore environment for eval mode (Windows)
func cleanEnvEvalWindows(out *tui.Output) error {
	shell := detectWindowsShell()
	if shell == shellPowerShell {
		return cleanEnvEvalPowerShell(out)
	}
	return cleanEnvEvalCMD(out)
}

// cleanEnvEvalPowerShell outputs PowerShell commands to restore environment
func cleanEnvEvalPowerShell(out *tui.Output) error {
	var script strings.Builder

	// Check if environment is active
	script.WriteString("if (-not $env:_GGO_ACTIVE) {\n")
	script.WriteString("  [Console]::Error.WriteLine('GPU Go environment is not active')\n")
	script.WriteString("} else {\n")

	// Restore original PATH
	script.WriteString("  if ($env:_GGO_ORIG_PATH) {\n")
	script.WriteString("    $env:PATH = $env:_GGO_ORIG_PATH\n")
	script.WriteString("  }\n\n")

	// Unset TensorFusion environment variables
	script.WriteString("  Remove-Item Env:TENSOR_FUSION_OPERATOR_CONNECTION_INFO -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:TF_LOG_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:TF_LOG_LEVEL -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:TF_ENABLE_LOG -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:TF_GPU_VENDOR -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:CUDA_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:CUDA_HOME -ErrorAction SilentlyContinue\n\n")

	// Unset internal tracking variables
	script.WriteString("  Remove-Item Env:_GGO_ORIG_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:_GGO_ACTIVE -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:_GGO_CACHE_PATH -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Item Env:_GGO_CLEAN_FILE -ErrorAction SilentlyContinue\n\n")

	// Remove ggo wrapper function (use Global scope since we defined it as Global)
	script.WriteString("  Remove-Item Function:ggo -ErrorAction SilentlyContinue\n")
	script.WriteString("  Remove-Variable _ggo_real -Scope Global -ErrorAction SilentlyContinue\n\n")

	script.WriteString("  [Console]::Error.WriteLine('GPU Go environment deactivated')\n")
	script.WriteString("}\n")

	// Output to stdout for eval
	fmt.Print(script.String())
	return nil
}

// cleanEnvEvalCMD outputs guidance for CMD users to restore environment
// Note: CMD doesn't support eval-style command execution
func cleanEnvEvalCMD(out *tui.Output) error {
	// Check if clean script exists
	cleanFile := os.Getenv("_GGO_CLEAN_FILE")
	if cleanFile == "" {
		fmt.Fprintf(os.Stderr, "GPU Go environment is not active or clean file not found.\n")
		return nil
	}

	// Output message to stderr
	fmt.Fprintf(os.Stderr, "CMD does not support eval-style deactivation.\n")
	fmt.Fprintf(os.Stderr, "Please run the following command to deactivate GPU environment:\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "  call \"%s\"\n", cleanFile)
	fmt.Fprintf(os.Stderr, "\n")

	// Output the call command to stdout so user can copy-paste
	fmt.Printf("call \"%s\"\n", cleanFile)
	return nil
}

// cleanCurrentEnv shows instructions or outputs clean commands for cleaning current shell environment
// When environment is activated (wrapper function defined), running "ggo clean" will automatically handle cleanup
func cleanCurrentEnv(out *tui.Output) error {
	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		// Print prompt to stderr so it doesn't interfere with eval
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, styles.Subtitle.Render("Clean GPU Go Environment"))
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, "Would you like to deactivate GPU environment in your current shell? [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		shouldClean := isYesResponse(response)

		if shouldClean {
			// User confirmed cleanup - need to guide them to use eval
			// Direct command execution doesn't work because we need shell to eval the unset commands
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, styles.Subtitle.Render("To deactivate, run:"))
			fmt.Fprintln(os.Stderr)
			if platform.IsWindows() {
				shell := detectWindowsShell()
				if shell == shellPowerShell {
					fmt.Fprintln(os.Stderr, "   ggo clean -y | Out-String | Invoke-Expression")
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "Or if you activated via 'ggo use ... -y | Out-String | Invoke-Expression', just run:")
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "   ggo clean")
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "(The wrapper function will handle it automatically)")
				} else {
					// CMD user - guide to use clean.bat
					cleanFile := os.Getenv("_GGO_CLEAN_FILE")
					if cleanFile != "" {
						fmt.Fprintf(os.Stderr, "   call \"%s\"\n", cleanFile)
					} else {
						fmt.Fprintln(os.Stderr, "   call clean.bat (in ggo config directory)")
					}
					fmt.Fprintln(os.Stderr)
					fmt.Fprintln(os.Stderr, "Or switch to PowerShell for better experience.")
				}
			} else {
				fmt.Fprintln(os.Stderr, "   eval \"$(ggo clean -y)\"")
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "Or if you activated via 'eval \"$(ggo use ...)\"', just run:")
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "   ggo clean")
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "(The wrapper function will handle it automatically)")
			}
			fmt.Fprintln(os.Stderr)
			return nil
		}

		// User chose not to clean, show how to clean later
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "You can deactivate later by running:")
		fmt.Fprintln(os.Stderr)
		if platform.IsWindows() {
			shell := detectWindowsShell()
			if shell == shellPowerShell {
				fmt.Fprintln(os.Stderr, "   ggo clean    (if environment is activated)")
			} else {
				cleanFile := os.Getenv("_GGO_CLEAN_FILE")
				if cleanFile != "" {
					fmt.Fprintf(os.Stderr, "   call \"%s\"\n", cleanFile)
				} else {
					fmt.Fprintln(os.Stderr, "   call clean.bat (in ggo config directory)")
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "   ggo clean    (if environment is activated)")
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "To clean up all GPU Go connections (including shell profiles):")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "   ggo clean --all")
		fmt.Fprintln(os.Stderr)
	}

	return nil
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

	// On Windows, also clean up permanent environment variables set by setx
	if platform.IsWindows() {
		removePermanentWinEnv()
	}

	return out.Render(&cleanAllResult{})
}

// removeFromShellProfiles removes GPU Go source lines from shell profiles
func removeFromShellProfiles() {
	if platform.IsWindows() {
		removeFromPowerShellProfile()
		return
	}

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

// removeFromPowerShellProfile removes GPU Go source lines from PowerShell profile
func removeFromPowerShellProfile() {
	// Check standard PowerShell profile locations
	// 1. Current User, All Hosts
	// 2. Current User, Current Host

	// We primarily target the one we added to, which is likely $PROFILE (Current User, Current Host)
	// But we can check standard paths.

	// Get My Documents path
	home := os.Getenv("USERPROFILE")
	if home == "" {
		return
	}

	docs := filepath.Join(home, "Documents")
	// Check for OneDrive
	oneDriveDocs := filepath.Join(home, "OneDrive", "Documents")

	candidates := []string{
		filepath.Join(docs, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
		filepath.Join(docs, "PowerShell", "Microsoft.PowerShell_profile.ps1"),
		filepath.Join(oneDriveDocs, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
		filepath.Join(oneDriveDocs, "PowerShell", "Microsoft.PowerShell_profile.ps1"),
	}

	// Also check $PROFILE env if set and distinct
	if profileEnv := os.Getenv("PROFILE"); profileEnv != "" {
		candidates = append(candidates, profileEnv)
	}

	marker := "# GPU Go environment"
	seen := make(map[string]bool)

	for _, profile := range candidates {
		if seen[profile] {
			continue
		}
		seen[profile] = true

		// removeLineFromFile handles file reading/existence checks
		removeLineFromFile(profile, marker)
	}
}

// removePermanentWinEnv removes permanent environment variables on Windows
func removePermanentWinEnv() {
	// List of variables we set in setenv.bat
	vars := []string{
		"TENSOR_FUSION_OPERATOR_CONNECTION_INFO",
		"TF_LOG_PATH",
		"TF_LOG_LEVEL",
		"TF_ENABLE_LOG",
		"TF_GPU_VENDOR",
		"CUDA_PATH",
		"CUDA_HOME",
	}

	for _, v := range vars {
		// Use reg delete to remove from HKCU\Environment
		// reg delete HKCU\Environment /F /V VariableName
		cmd := exec.Command("reg", "delete", "HKCU\\Environment", "/F", "/V", v)
		// We ignore errors as the variable might not exist
		_ = cmd.Run()
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
		trimmed := strings.TrimSpace(line)
		// Support both Unix "source" and PowerShell "." (dot sourcing)
		if skipNext && (strings.HasPrefix(trimmed, "source ") || strings.HasPrefix(trimmed, ".")) {
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
	out.Println("Note: Environment variables in your current shell may still be set.")
	out.Println()
	if platform.IsWindows() {
		shell := detectWindowsShell()
		if shell == shellPowerShell {
			out.Println("Start a new shell or run:")
			out.Println()
			out.Println("   ggo clean -y | Out-String | Invoke-Expression")
			out.Println()
		} else {
			out.Println("Start a new CMD window to get a clean environment.")
		}
	} else {
		out.Println("To clean up your current shell environment, run:")
		out.Println()
		out.Println("   ggo clean")
		out.Println()
		out.Println("This will properly restore LD_PRELOAD, LD_LIBRARY_PATH, and PATH.")
	}
	out.Println()
}

// Silence unused import warning
var _ = exec.Command
