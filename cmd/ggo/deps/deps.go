package deps

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	cdnURL          string
	apiURL          string
	force           bool
	verbose         bool
	syncOS          string
	syncArch        string
	listOS          string
	listArch        string
	downloadName    string
	downloadVersion string
	downloadOS      string
	downloadArch    string
)

// NewDepsCmd creates the deps command
func NewDepsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage vGPU library dependencies",
		Long:  `Download and manage vGPU library dependencies (libcuda.so, libnvidia-ml.so, etc.)`,
	}

	cmd.PersistentFlags().StringVar(&cdnURL, "cdn", deps.DefaultCDNBaseURL, "CDN base URL")
	cmd.PersistentFlags().StringVar(&apiURL, "api", api.GetDefaultBaseURL(), "API base URL (or set GPU_GO_ENDPOINT env var)")

	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDownloadCmd())
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newCleanCmd())

	return cmd
}

func getManager() *deps.Manager {
	return deps.NewManager(
		deps.WithCDNBaseURL(cdnURL),
		deps.WithAPIBaseURL(apiURL),
	)
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync releases metadata from API",
		Long:  `Fetch vendor releases from the API and cache them locally as version metadata.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			ctx := context.Background()

			targetOS := syncOS
			targetArch := syncArch
			if targetOS != "" || targetArch != "" {
				fmt.Printf("Syncing releases from API for platform %s/%s...\n", targetOS, targetArch)
			} else {
				fmt.Println("Syncing releases from API...")
			}

			manifest, err := mgr.SyncReleases(ctx, targetOS, targetArch)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to sync releases: error=%v", err)
				return err
			}

			if manifest != nil {
				fmt.Printf("Synced %d libraries (manifest version: %s)\n", len(manifest.Libraries), manifest.Version)

				if len(manifest.Libraries) > 0 {
					if verbose {
						fmt.Println("\nSynced libraries:")
						for _, lib := range manifest.Libraries {
							fmt.Printf("  Name: %s\n", lib.Name)
							fmt.Printf("    Version: %s\n", lib.Version)
							fmt.Printf("    Platform: %s/%s\n", lib.Platform, lib.Arch)
							fmt.Printf("    Size: %d bytes\n", lib.Size)
							fmt.Printf("    SHA256: %s\n", lib.SHA256)
							fmt.Printf("    URL: %s\n", lib.URL)
							fmt.Println()
						}
					} else {
						fmt.Println("\nSynced libraries:")
						for _, lib := range manifest.Libraries {
							fmt.Printf("  %s (version: %s, platform: %s/%s)\n", lib.Name, lib.Version, lib.Platform, lib.Arch)
						}
					}
				}
			}

			fmt.Println("Sync complete!")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print verbose synced data")
	cmd.Flags().StringVar(&syncOS, "os", "", "Target OS (linux, darwin, windows). Defaults to current OS")
	cmd.Flags().StringVar(&syncArch, "arch", "", "Target architecture (amd64, arm64). Defaults to current architecture")
	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available and installed dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			ctx := context.Background()

			// Get installed libraries
			installed, err := mgr.GetInstalledLibraries()
			if err != nil {
				klog.Warningf("Failed to load installed libraries: error=%v", err)
			}

			// Get downloaded libraries
			downloaded, err := mgr.GetDownloadedLibraries()
			if err != nil {
				klog.Warningf("Failed to load downloaded libraries: error=%v", err)
			}

			// Fetch available libraries (from cached manifest or sync)
			manifest, synced, err := mgr.FetchManifest(ctx)
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			if synced {
				// Check for updates after a sync
				updates, _ := mgr.CheckUpdates(ctx)
				if len(updates) > 0 {
					fmt.Printf("\n%s\n", tui.InfoMessage(fmt.Sprintf("Manifest updated. %d updates available. Run 'ggo deps update' to upgrade.", len(updates))))
				}
			}

			// Determine if we should list all architectures or filter
			var libs []deps.Library
			var filterDesc string
			if listOS == "" && listArch == "" {
				// List all architectures
				libs = mgr.GetAllLibraries(manifest)
				filterDesc = "all platforms"
			} else {
				// Filter by specified OS/Arch (empty string means current platform)
				libs = mgr.GetLibrariesForPlatform(manifest, listOS, listArch, "")
				if listOS != "" && listArch != "" {
					filterDesc = fmt.Sprintf("%s/%s", listOS, listArch)
				} else if listOS != "" {
					filterDesc = fmt.Sprintf("%s/*", listOS)
				} else {
					filterDesc = fmt.Sprintf("*/%s", listArch)
				}
			}

			if len(libs) == 0 {
				if filterDesc != "all platforms" {
					fmt.Printf("No libraries available for platform %s\n", filterDesc)
				} else {
					fmt.Println("No libraries available")
				}
				return nil
			}

			// Use TUI table
			headers := []string{"Name", "Version", "Platform", "Size", "Status"}
			tb := tui.NewTable().Headers(headers...)

			// Sort libs by name for consistent output
			slices.SortFunc(libs, func(a, b deps.Library) int {
				if a.Name != b.Name {
					return compareStrings(a.Name, b.Name)
				}
				return compareStrings(a.Platform, b.Platform)
			})

			for _, lib := range libs {
				status := "Available"
				style := tui.DefaultStyles().Muted

				// Check installed status
				isInstalled := false
				if installedLib, exists := installed.Libraries[lib.Name]; exists {
					if installedLib.Version == lib.Version && installedLib.Platform == lib.Platform && installedLib.Arch == lib.Arch {
						status = "Installed"
						style = tui.DefaultStyles().Success
						isInstalled = true
					} else if installedLib.Version != lib.Version {
						status = fmt.Sprintf("Update: %s -> %s", installedLib.Version, lib.Version)
						style = tui.DefaultStyles().Warning
					}
				}

				// Check downloaded status (if not installed)
				if !isInstalled {
					// Check if downloaded
					if downloadedLib, exists := downloaded.Libraries[lib.Name]; exists {
						if downloadedLib.Version == lib.Version && downloadedLib.Platform == lib.Platform && downloadedLib.Arch == lib.Arch {
							status = "Downloaded"
							style = tui.DefaultStyles().Info
						}
					}
				}

				tb.Row(
					lib.Name,
					lib.Version,
					fmt.Sprintf("%s/%s", lib.Platform, lib.Arch),
					formatSize(lib.Size),
					style.Render(status),
				)
			}

			fmt.Println(tb.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&listOS, "os", "", "Filter by OS (linux, darwin, windows). Omit to list all architectures")
	cmd.Flags().StringVar(&listArch, "arch", "", "Filter by architecture (amd64, arm64). Omit to list all architectures")
	return cmd
}

func compareStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download [library...]",
		Short: "Download dependencies to cache",
		Long:  `Download vGPU library dependencies to the local cache without installing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			ctx := context.Background()

			manifest, _, err := mgr.FetchManifest(ctx)
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			// Determine target platform from flags or use current platform
			targetOS := downloadOS
			targetArch := downloadArch

			// Get libraries for the target platform
			libs := mgr.GetLibrariesForPlatform(manifest, targetOS, targetArch, "")
			if len(libs) == 0 {
				platformDesc := "this platform"
				if targetOS != "" || targetArch != "" {
					platformDesc = fmt.Sprintf("%s/%s", targetOS, targetArch)
				}
				fmt.Printf("No libraries available for %s\n", platformDesc)
				return nil
			}

			// Apply filters
			filtered := []deps.Library{}
			for _, lib := range libs {
				// Filter by name: --name flag takes precedence, otherwise use args
				if downloadName != "" {
					if lib.Name != downloadName {
						continue
					}
				} else if len(args) > 0 {
					// Match any of the provided names in args
					matched := slices.Contains(args, lib.Name)
					if !matched {
						continue
					}
				}

				// Filter by version
				if downloadVersion != "" && lib.Version != downloadVersion {
					continue
				}

				filtered = append(filtered, lib)
			}

			if len(filtered) == 0 {
				fmt.Println("No libraries match the specified criteria")
				return nil
			}

			libs = filtered

			for _, lib := range libs {
				fmt.Printf("Downloading %s (version: %s, platform: %s/%s)...\n", lib.Name, lib.Version, lib.Platform, lib.Arch)

				progressFn := func(downloaded, total int64) {
					if total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Progress: %.1f%% (%d/%d bytes)", pct, downloaded, total)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					// Runtime error - don't show help
					cmd.SilenceUsage = true
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					return err
				}
				fmt.Println("\n  Done!")
			}

			fmt.Println("\nAll downloads complete!")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-download even if cached")
	cmd.Flags().StringVar(&downloadName, "name", "", "Library name to download (e.g., libcuda.so.1)")
	cmd.Flags().StringVar(&downloadVersion, "version", "", "Library version to download")
	cmd.Flags().StringVar(&downloadOS, "os", "", "Target OS (linux, darwin, windows). Defaults to current OS")
	cmd.Flags().StringVar(&downloadArch, "cpuArch", "", "Target CPU architecture (amd64, arm64). Defaults to current architecture")
	return cmd
}

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [library...]",
		Short: "Download and install dependencies",
		Long:  `Download and install vGPU library dependencies to the system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			ctx := context.Background()

			manifest, _, err := mgr.FetchManifest(ctx)
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			libs := mgr.GetLibrariesForPlatform(manifest, "", "", "")
			if len(libs) == 0 {
				fmt.Println("No libraries available for this platform")
				return nil
			}

			// Filter by args if provided
			if len(args) > 0 {
				filtered := []deps.Library{}
				for _, lib := range libs {
					for _, name := range args {
						if lib.Name == name {
							filtered = append(filtered, lib)
							break
						}
					}
				}
				libs = filtered
			}

			for _, lib := range libs {
				fmt.Printf("Installing %s (version: %s)...\n", lib.Name, lib.Version)

				// Download first
				progressFn := func(downloaded, total int64) {
					if total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Downloading: %.1f%%", pct)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					// Runtime error - don't show help
					cmd.SilenceUsage = true
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					return err
				}
				fmt.Println()

				// Then install
				fmt.Printf("  Installing to %s...\n", mgr.GetLibraryPath(lib.Name))
				if err := mgr.InstallLibrary(lib); err != nil {
					// Runtime error - don't show help
					cmd.SilenceUsage = true
					klog.Errorf("Failed to install: library=%s error=%v", lib.Name, err)
					return err
				}
				fmt.Println("  Done!")
			}

			fmt.Println("\nAll libraries installed!")
			fmt.Println("\nTo use the libraries, add the following to your environment:")
			fmt.Printf("  export LD_PRELOAD=%s\n", mgr.GetLibraryPath("libcuda.so.1"))

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force reinstall even if already installed")
	return cmd
}

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			ctx := context.Background()

			fmt.Println("Checking for updates...")
			updates, err := mgr.CheckUpdates(ctx)
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				klog.Errorf("Failed to check updates: error=%v", err)
				return err
			}

			if len(updates) == 0 {
				fmt.Println("All libraries are up to date!")
				return nil
			}

			fmt.Printf("Found %d updates:\n", len(updates))
			for _, lib := range updates {
				fmt.Printf("  %s: %s\n", lib.Name, lib.Version)
			}

			fmt.Println("\nInstalling updates...")
			for _, lib := range updates {
				fmt.Printf("Updating %s...\n", lib.Name)

				progressFn := func(downloaded, total int64) {
					if total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Progress: %.1f%%", pct)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					continue
				}
				fmt.Println()

				if err := mgr.InstallLibrary(lib); err != nil {
					klog.Errorf("Failed to install: library=%s error=%v", lib.Name, err)
					continue
				}
				fmt.Println("  Done!")
			}

			fmt.Println("\nAll updates installed!")
			return nil
		},
	}
	return cmd
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean the dependency cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()

			fmt.Println("Cleaning dependency cache...")
			if err := mgr.CleanCache(); err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				klog.Errorf("Failed to clean cache: error=%v", err)
				return err
			}

			fmt.Println("Cache cleaned!")
			return nil
		},
	}
	return cmd
}

func init() {
	// Suppress unused warning for os package
	_ = os.Stderr
}
