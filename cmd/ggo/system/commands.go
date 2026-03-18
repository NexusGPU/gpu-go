package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// NewUpdateCmd creates the update command.
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Update ggo and dependencies to the latest version",
		Long:    "Updates the ggo CLI binary via CDN install script, then updates vGPU library dependencies.",
		Example: "ggo update",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: Update the CLI binary
			command, cmdArgs, err := buildScriptCommand(scriptActionUpdate, runtime.GOOS)
			if err != nil {
				return err
			}
			execCmd := exec.Command(command, cmdArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin
			if err := execCmd.Run(); err != nil {
				return fmt.Errorf("failed to update ggo binary: %w", err)
			}

			// Step 2: Update dependencies
			fmt.Println("\nUpdating dependencies...")
			if err := updateDeps(); err != nil {
				klog.Warningf("Failed to update dependencies: %v", err)
				fmt.Printf("Warning: dependency update failed: %v\n", err)
				fmt.Println("You can update dependencies manually with: ggo deps update -y")
			}

			return nil
		},
	}

	return cmd
}

// updateDeps syncs and downloads the latest dependencies.
func updateDeps() error {
	paths := platform.DefaultPaths()
	mgr := deps.NewManager(deps.WithPaths(paths))
	ctx := context.Background()

	// Update deps manifest (syncs releases internally)
	_, _, err := mgr.UpdateDepsManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to update deps manifest: %w", err)
	}

	// Compute diff
	diff, err := mgr.ComputeUpdateDiff()
	if err != nil {
		return fmt.Errorf("failed to compute update diff: %w", err)
	}

	if len(diff.ToDownload) == 0 {
		fmt.Println("All dependencies are up to date!")
		return nil
	}

	fmt.Printf("Downloading %d dependency update(s)...\n", len(diff.ToDownload))
	progressFn := func(lib deps.Library, downloaded, total int64) {
		if total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}
	results, err := mgr.DownloadAllRequired(ctx, progressFn)
	if err != nil {
		return fmt.Errorf("failed to download dependencies: %w", err)
	}

	fmt.Println()
	successCount := 0
	for _, r := range results {
		if r.Status != deps.DownloadStatusFailed {
			successCount++
		}
	}
	fmt.Printf("Dependencies updated: %d/%d successful\n", successCount, len(results))
	return nil
}

// NewUninstallCmd creates the uninstall command.
func NewUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "uninstall",
		Short:   "Uninstall ggo from this machine",
		Long:    "Cleans up local data and downloads the platform uninstall script from the CDN.",
		Example: "ggo uninstall",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: Clean up local data directory that CDN script might miss
			paths := platform.DefaultPaths()
			homeDir := paths.UserDir()
			if homeDir != "" {
				if _, err := os.Stat(homeDir); err == nil {
					fmt.Printf("Removing %s...\n", homeDir)
					if err := os.RemoveAll(homeDir); err != nil {
						klog.Warningf("Failed to remove %s: %v", homeDir, err)
						fmt.Printf("Warning: failed to remove %s: %v\n", homeDir, err)
					}
				}
			}

			// Step 2: Run CDN uninstall script
			command, cmdArgs, err := buildScriptCommand(scriptActionUninstall, runtime.GOOS)
			if err != nil {
				return err
			}
			execCmd := exec.Command(command, cmdArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin
			return execCmd.Run()
		},
	}

	return cmd
}
