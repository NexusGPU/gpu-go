//go:build windows

package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	setDllDirectoryW = kernel32.NewProc("SetDllDirectoryW")
)

// SetDllDirectory sets the DLL search path for the process
// This ensures our stub DLLs are loaded before System32 DLLs
func SetDllDirectory(path string) error {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("failed to convert path to UTF16: %w", err)
	}

	ret, _, err := setDllDirectoryW.Call(uintptr(unsafe.Pointer(pathPtr)))
	if ret == 0 {
		return fmt.Errorf("SetDllDirectoryW failed: %w", err)
	}
	return nil
}

// NewLaunchCmd creates the launch command (Windows only)
func NewLaunchCmd() *cobra.Command {
	var (
		verbose bool
	)

	cmd := &cobra.Command{
		Use:   "launch <program> [args...]",
		Short: "Launch a program with GPU libraries pre-loaded (Windows only)",
		Long: `Launch a program with TensorFusion GPU libraries properly loaded.

This command uses SetDllDirectory to ensure the stub DLLs (nvcuda.dll, nvml.dll)
from the gpugo cache are loaded instead of the real NVIDIA drivers in System32.

Prerequisites:
  - Run 'ggo use <share-link>' first to configure the GPU connection
  - Run 'ggo deps sync && ggo deps download' to download required libraries

Examples:
  # Launch Python with remote GPU
  ggo launch python train.py

  # Launch Jupyter notebook
  ggo launch jupyter notebook

  # Launch with arguments
  ggo launch python -c "import torch; print(torch.cuda.is_available())"

  # Launch any executable
  ggo launch myapp.exe --config config.yaml`,
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

	// Get cache directory
	cacheDir := paths.CacheDir()

	// Check if cache directory exists and has DLLs
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		out.Println()
		out.Warning("GPU libraries not downloaded!")
		out.Println()
		out.Println("Please run the following commands first:")
		out.Println("  ggo deps sync")
		out.Println("  ggo deps download")
		out.Println()
		return fmt.Errorf("cache directory does not exist: %s", cacheDir)
	}

	// Detect vendor from TF_GPU_VENDOR env var (set by ggo use)
	// Default to NVIDIA if not specified
	vendorStr := os.Getenv("TF_GPU_VENDOR")
	if vendorStr == "" {
		vendorStr = "nvidia" // default
	}
	vendor := studio.ParseVendor(vendorStr)

	// Get required DLLs based on vendor
	requiredDLLs := studio.GetWindowsLibraryNames(vendor)
	missingDLLs := []string{}
	for _, dll := range requiredDLLs {
		dllPath := filepath.Join(cacheDir, dll)
		if _, err := os.Stat(dllPath); os.IsNotExist(err) {
			missingDLLs = append(missingDLLs, dll)
		}
	}

	if len(missingDLLs) > 0 {
		out.Println()
		out.Warning("Missing required DLLs in cache!")
		out.Println()
		out.Printf("Vendor:  %s\n", vendor)
		out.Printf("Missing: %s\n", strings.Join(missingDLLs, ", "))
		out.Println()
		out.Println("Please run:")
		out.Println("  ggo deps sync")
		out.Println("  ggo deps download")
		out.Println()
		// Continue anyway - the DLLs might be named differently or not yet available
		klog.Warningf("Missing DLLs for vendor %s (continuing anyway): %v", vendor, missingDLLs)
	}

	// Set DLL search directory BEFORE launching the process
	// This ensures our stub DLLs are found before System32
	if err := SetDllDirectory(cacheDir); err != nil {
		return fmt.Errorf("failed to set DLL directory: %w", err)
	}

	if verbose {
		out.Println()
		out.Printf("%s Launching with GPU libraries from: %s\n",
			styles.Info.Render("◐"), cacheDir)
		out.Printf("   Connection: %s\n", connectionInfo)
		out.Println()
	}

	klog.Infof("SetDllDirectory set to: %s", cacheDir)
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

	// Inherit environment (which includes TENSOR_FUSION_OPERATOR_CONNECTION_INFO)
	execCmd.Env = os.Environ()

	// Run the command
	if err := execCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("failed to run program: %w", err)
	}

	return nil
}
