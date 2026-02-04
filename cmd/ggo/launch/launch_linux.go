//go:build linux

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// NewLaunchCmd creates the launch command (Linux)
func NewLaunchCmd() *cobra.Command {
	var (
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "launch <program> [args...]",
		Short: "Launch a program with GPU libraries pre-loaded",
		Long: `Launch a program with TensorFusion GPU libraries properly loaded.

This command sets up LD_PRELOAD and LD_LIBRARY_PATH to ensure the GPU client
libraries from the gpugo cache are loaded for the target program.

Prerequisites:
  - Run 'ggo use <share-link>' first to configure the GPU connection

Examples:
  # Launch Python with remote GPU
  ggo launch python train.py

  # Launch Jupyter notebook
  ggo launch jupyter notebook

  # Launch with arguments
  ggo launch python -c "import torch; print(torch.cuda.is_available())"

  # Launch any executable
  ggo launch ./my-gpu-app --config config.yaml`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true, // Allow passing all flags to the child process
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLaunch(args, verbose)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	return cmd
}

func runLaunch(args []string, verbose bool) error {
	paths := platform.DefaultPaths()
	out := cmdutil.NewOutput("table")
	styles := tui.DefaultStyles()
	ctx := context.Background()

	// Check if TENSOR_FUSION_OPERATOR_CONNECTION_INFO is set
	connectionInfo := os.Getenv("TENSOR_FUSION_OPERATOR_CONNECTION_INFO")
	if connectionInfo == "" {
		out.Println()
		out.Warning("GPU connection not configured!")
		out.Println()
		out.Println("Please run 'ggo use <share-link>' first to configure the GPU connection.")
		out.Println()
		out.Println("Example:")
		out.Println("  ggo use abc123")
		out.Println("  ggo launch python train.py")
		out.Println()
		return fmt.Errorf("TENSOR_FUSION_OPERATOR_CONNECTION_INFO not set")
	}

	// Detect vendor from TF_GPU_VENDOR env var (set by ggo use)
	vendorStr := os.Getenv("TF_GPU_VENDOR")
	if vendorStr == "" {
		vendorStr = "nvidia" // default
	}
	vendor := studio.ParseVendor(vendorStr)

	// Ensure required GPU client libraries exist (same as ggo use)
	// This will auto-sync and download if needed
	if err := ensureRemoteGPUClientLibs(ctx, out, verbose); err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	// Get cache directory
	cacheDir := paths.CacheDir()

	// Get required libraries based on vendor
	requiredLibs := studio.GetLibraryNames(vendor)

	// Verify libraries exist after ensuring they're downloaded
	missingLibs := []string{}
	for _, lib := range requiredLibs {
		libPath := filepath.Join(cacheDir, lib)
		if _, err := os.Stat(libPath); os.IsNotExist(err) {
			missingLibs = append(missingLibs, lib)
		}
	}

	if len(missingLibs) > 0 {
		out.Println()
		out.Warning("Missing required libraries in cache!")
		out.Println()
		out.Printf("Vendor:  %s\n", vendor)
		out.Printf("Missing: %s\n", strings.Join(missingLibs, ", "))
		out.Println()
		out.Println("Please run:")
		out.Println("  ggo deps sync")
		out.Println("  ggo deps download")
		out.Println()
		// Continue anyway - the libraries might be named differently or not yet available
		klog.Warningf("Missing libraries for vendor %s (continuing anyway): %v", vendor, missingLibs)
	}

	// Setup log path (consistent with ggo use)
	studioName := "current-os"
	logPath := paths.StudioLogsDir(studioName)
	if err := os.MkdirAll(logPath, 0755); err != nil {
		klog.Warningf("Failed to create log directory: %v", err)
	}

	if verbose {
		out.Println()
		out.Printf("%s Launching with GPU libraries from: %s\n",
			styles.Info.Render("◐"), cacheDir)
		out.Printf("   Connection: %s\n", connectionInfo)
		out.Printf("   Log Path:   %s\n", logPath)
		out.Println()
	}

	klog.Infof("LD_PRELOAD/LD_LIBRARY_PATH set to: %s", cacheDir)
	klog.Infof("Launching: %v", args)

	// Find the executable
	program := args[0]
	programArgs := []string{}
	if len(args) > 1 {
		programArgs = args[1:]
	}

	// Look up the program in PATH if not an absolute path
	execPath, err := exec.LookPath(program)
	if err != nil {
		return fmt.Errorf("program not found: %s", program)
	}

	// Create the command
	execCmd := exec.Command(execPath, programArgs...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	// Build environment with GPU settings (consistent with ggo use)
	env := os.Environ()

	// Set TensorFusion environment variables (consistent with ggo use / studio.SetupGPUEnv)
	env = setEnvVar(env, "TENSOR_FUSION_OPERATOR_CONNECTION_INFO", connectionInfo)
	env = setEnvVar(env, "TF_LOG_PATH", logPath)
	env = setEnvVar(env, "TF_LOG_LEVEL", getEnvDefault("TF_LOG_LEVEL", "info"))
	env = setEnvVar(env, "TF_ENABLE_LOG", getEnvDefault("TF_ENABLE_LOG", "0"))
	env = setEnvVar(env, "TF_GPU_VENDOR", string(vendor))

	// Build LD_LIBRARY_PATH
	existingLDPath := os.Getenv("LD_LIBRARY_PATH")
	if existingLDPath != "" {
		env = setEnvVar(env, "LD_LIBRARY_PATH", cacheDir+":"+existingLDPath)
	} else {
		env = setEnvVar(env, "LD_LIBRARY_PATH", cacheDir)
	}

	// Build LD_PRELOAD with library paths
	if len(requiredLibs) > 0 {
		var preloadPaths []string
		for _, lib := range requiredLibs {
			preloadPaths = append(preloadPaths, filepath.Join(cacheDir, lib))
		}
		existingPreload := os.Getenv("LD_PRELOAD")
		if existingPreload != "" {
			env = setEnvVar(env, "LD_PRELOAD", strings.Join(preloadPaths, ":")+":"+existingPreload)
		} else {
			env = setEnvVar(env, "LD_PRELOAD", strings.Join(preloadPaths, ":"))
		}
	}

	execCmd.Env = env

	// Run the command
	if err := execCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("failed to run program: %w", err)
	}

	return nil
}

// ensureRemoteGPUClientLibs downloads remote-gpu-client libraries if not already present
// This is consistent with ggo use command behavior
func ensureRemoteGPUClientLibs(ctx context.Context, out *tui.Output, verbose bool) error {
	depsMgr := deps.NewManager()

	// Target library types that are needed for GPU client functionality
	// Same as ggo use command
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	if verbose {
		out.Printf("Checking GPU client libraries...\n")
	}

	progressFn := func(lib deps.Library, downloaded, total int64) {
		if verbose && total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}

	libs, err := depsMgr.EnsureLibrariesByTypes(ctx, targetTypes, progressFn)
	if err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	if verbose {
		if len(libs) > 0 {
			fmt.Println()
			out.Success("GPU client libraries ready!")
		} else {
			klog.V(4).Info("All required GPU client libraries are already downloaded")
		}
	}

	return nil
}

// getEnvDefault gets an environment variable or returns a default value
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// setEnvVar sets or updates an environment variable in the env slice
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
