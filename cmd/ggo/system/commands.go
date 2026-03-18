package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
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
		Long:    "Unregisters the agent, stops services, removes binary and local data.",
		Example: "ggo uninstall",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: Unregister agent from server (must happen before config is removed)
			tryUnregisterAgent()

			// Step 2: Clean up data directories (after unregister read the config,
			// but before CDN script which may kill this process)
			cleanupDataDirs()

			// Step 3: Run CDN uninstall script (stops services, removes binary)
			// NOTE: this script kills all ggo processes, so this must be last
			command, cmdArgs, err := buildScriptCommand(scriptActionUninstall, runtime.GOOS)
			if err != nil {
				return err
			}
			execCmd := exec.Command(command, cmdArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin
			if err := execCmd.Run(); err != nil {
				klog.Warningf("Uninstall script failed: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// cleanupDataDirs removes ggo data directories for both the current user and root.
// When running as a normal user, it removes the user's own ~/.gpugo directly,
// then uses sudo to remove root's ~/.gpugo (prompting for password if needed).
func cleanupDataDirs() {
	// Remove current user's data directory
	paths := platform.DefaultPaths()
	userDir := paths.UserDir()
	if userDir != "" {
		if _, err := os.Stat(userDir); err == nil {
			fmt.Printf("Removing %s...\n", userDir)
			if err := os.RemoveAll(userDir); err != nil {
				klog.Warningf("Failed to remove %s: %v", userDir, err)
			}
		}
	}

	if platform.IsWindows() {
		return
	}

	// If running as root, /root/.gpugo is already handled above
	if os.Getuid() == 0 {
		return
	}

	// Running as normal user — the agent runs as root via systemd/launchd,
	// so root's ~/.gpugo needs sudo to remove
	rootGpugoDir := filepath.Join(rootHomeDir(), ".gpugo")
	// Check if the directory exists (may fail without sudo, that's ok)
	checkCmd := exec.Command("sudo", "-n", "test", "-d", rootGpugoDir)
	if checkCmd.Run() != nil {
		// sudo -n failed (needs password) or dir doesn't exist; try with password prompt
		checkCmd2 := exec.Command("sudo", "test", "-d", rootGpugoDir)
		checkCmd2.Stdin = os.Stdin
		if checkCmd2.Run() != nil {
			return
		}
	}

	fmt.Printf("Removing %s (requires sudo)...\n", rootGpugoDir)
	rmCmd := exec.Command("sudo", "rm", "-rf", rootGpugoDir)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	rmCmd.Stdin = os.Stdin
	if err := rmCmd.Run(); err != nil {
		klog.Warningf("Failed to remove %s: %v", rootGpugoDir, err)
		fmt.Printf("Warning: failed to remove %s, you may need to run: sudo rm -rf %s\n", rootGpugoDir, rootGpugoDir)
	}
}

// rootHomeDir returns the home directory of the root user on Unix systems.
func rootHomeDir() string {
	if runtime.GOOS == "darwin" {
		return "/var/root"
	}
	return "/root"
}

// tryUnregisterAgent attempts to unregister the agent from the server before cleanup.
// It checks the current user's config first, then root's config on Unix systems
// (since the agent typically runs as root via systemd/launchd).
func tryUnregisterAgent() {
	// Try current user's config first
	cfgPaths := platform.DefaultPaths()
	configMgr := config.NewManager(cfgPaths.ConfigDir(), cfgPaths.StateDir())
	if unregisterFromConfig(configMgr) {
		return
	}

	// On Unix, if running as non-root, check root's config
	// (agent runs as root via systemd/launchd, so config lives in root's home)
	if platform.IsWindows() || os.Getuid() == 0 {
		return
	}

	tryUnregisterRootAgent()
}

// unregisterFromConfig loads config from the given manager and unregisters the agent.
// Returns true if a registered agent was found (regardless of unregister success).
func unregisterFromConfig(configMgr *config.Manager) bool {
	cfg, err := configMgr.LoadConfig()
	if err != nil || cfg == nil || cfg.AgentID == "" || cfg.AgentSecret == "" {
		return false
	}
	unregisterAgentFromServer(cfg)
	return true
}

// unregisterAgentFromServer calls the server API to delete the agent record.
func unregisterAgentFromServer(cfg *config.Config) {
	srvURL := cfg.ServerURL
	if srvURL == "" {
		srvURL = api.GetDefaultBaseURL()
	}

	fmt.Printf("Unregistering agent %s...\n", cfg.AgentID)
	client := api.NewClient(
		api.WithBaseURL(srvURL),
		api.WithAgentSecret(cfg.AgentSecret),
	)

	if err := client.SelfDeleteAgent(context.Background(), cfg.AgentID); err != nil {
		klog.Warningf("Failed to unregister agent from server: agent_id=%s error=%v", cfg.AgentID, err)
		fmt.Printf("Warning: could not unregister agent from server: %v\n", err)
	} else {
		fmt.Printf("Agent %s unregistered from server\n", cfg.AgentID)
	}
}

// tryUnregisterRootAgent reads the root user's agent config via sudo and
// unregisters it from the server. This handles the common case where the
// agent runs as root (systemd/launchd) but uninstall runs as a normal user.
func tryUnregisterRootAgent() {
	rootConfigPath := filepath.Join(rootHomeDir(), ".gpugo", "config", "config.json")

	data, err := readFileWithSudo(rootConfigPath)
	if err != nil {
		return
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		klog.Warningf("Failed to parse root agent config: %v", err)
		return
	}

	if cfg.AgentID == "" || cfg.AgentSecret == "" {
		return
	}

	unregisterAgentFromServer(&cfg)
}

// readFileWithSudo reads a file using sudo. It first tries without a password
// prompt (sudo -n), then falls back to prompting for the password.
func readFileWithSudo(path string) ([]byte, error) {
	// Try without password first
	if out, err := exec.Command("sudo", "-n", "cat", path).Output(); err == nil {
		return out, nil
	}

	// Try with password prompt
	cmd := exec.Command("sudo", "cat", path)
	cmd.Stdin = os.Stdin
	return cmd.Output()
}
