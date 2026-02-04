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
	"github.com/NexusGPU/gpu-go/internal/api"
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
		verbose   bool
		shareLink string
		serverURL string
	)

	cmd := &cobra.Command{
		Use:   "launch -s <share-link> <program> [args...]",
		Short: "Launch a program with GPU libraries pre-loaded",
		Long: `Launch a program with TensorFusion GPU libraries properly loaded.

This command sets up LD_PRELOAD and LD_LIBRARY_PATH to ensure the GPU client
libraries from the gpugo cache are loaded for the target program.

The -s flag is required to specify the share link for GPU connection.

Examples:
  # Launch Python with remote GPU
  ggo launch -s abc123 python train.py

  # Launch Jupyter notebook
  ggo launch -s https://go.gpu.tf/s/abc123 jupyter notebook

  # Launch with arguments
  ggo launch -s abc123 python -c "import torch; print(torch.cuda.is_available())"

  # Launch any executable
  ggo launch -s abc123 ./my-gpu-app --config config.yaml`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shareLink == "" {
				return fmt.Errorf("share link is required, use -s <share-link>")
			}
			return runLaunch(args, shareLink, serverURL, verbose)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")
	cmd.Flags().StringVarP(&shareLink, "share", "s", "", "Share link or short code for GPU connection (required)")
	cmd.Flags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	_ = cmd.MarkFlagRequired("share")

	return cmd
}

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

func runLaunch(args []string, shareLink, serverURL string, verbose bool) error {
	paths := platform.DefaultPaths()
	out := cmdutil.NewOutput("table")
	styles := tui.DefaultStyles()
	ctx := context.Background()

	// Get share info from API
	shortCode := extractShortCode(shareLink)
	client := api.NewClient(api.WithBaseURL(serverURL))
	shareInfo, err := client.GetSharePublic(ctx, shortCode)
	if err != nil {
		out.Println()
		out.Warning("Failed to get GPU share info!")
		out.Println()
		out.Printf("Share link: %s\n", shareLink)
		out.Printf("Error: %v\n", err)
		out.Println()
		return fmt.Errorf("failed to get share info: %w", err)
	}

	klog.Infof("Found GPU worker: worker_id=%s vendor=%s connection_url=%s", shareInfo.WorkerID, shareInfo.HardwareVendor, shareInfo.ConnectionURL)

	connectionInfo := shareInfo.ConnectionURL
	vendor := studio.ParseVendor(shareInfo.HardwareVendor)

	// Ensure required GPU client libraries exist
	// Filter by vendor from share info to avoid downloading unnecessary libraries
	if err := ensureRemoteGPUClientLibs(ctx, out, shareInfo.HardwareVendor, verbose); err != nil {
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
		out.Printf("   Vendor:     %s\n", vendor)
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
// vendorSlug filters by vendor (e.g., "nvidia", "amd") to avoid downloading unnecessary libraries
func ensureRemoteGPUClientLibs(ctx context.Context, out *tui.Output, vendorSlug string, verbose bool) error {
	depsMgr := deps.NewManager()

	// Target library types that are needed for GPU client functionality
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	if verbose {
		out.Printf("Checking GPU client libraries for %s...\n", vendorSlug)
	}

	progressFn := func(lib deps.Library, downloaded, total int64) {
		if verbose && total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}

	libs, err := depsMgr.EnsureLibrariesByTypes(ctx, targetTypes, vendorSlug, progressFn)
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
