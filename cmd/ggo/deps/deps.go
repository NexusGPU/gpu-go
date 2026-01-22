package deps

import (
	"context"
	"fmt"
	"os"

	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	cdnURL string
	force  bool
)

// NewDepsCmd creates the deps command
func NewDepsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage vGPU library dependencies",
		Long:  `Download and manage vGPU library dependencies (libcuda.so, libnvidia-ml.so, etc.)`,
	}

	cmd.PersistentFlags().StringVar(&cdnURL, "cdn", deps.DefaultCDNBaseURL, "CDN base URL")

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDownloadCmd())
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newCleanCmd())

	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available and installed dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := deps.NewManager(deps.WithCDNBaseURL(cdnURL))
			ctx := context.Background()

			// Get installed libraries
			installed, err := mgr.GetInstalledLibraries()
			if err != nil {
				log.Warn().Err(err).Msg("Failed to load installed libraries")
			}

			if len(installed.Libraries) > 0 {
				fmt.Println("Installed libraries:")
				for name, lib := range installed.Libraries {
					fmt.Printf("  %s (version: %s)\n", name, lib.Version)
				}
				fmt.Println()
			}

			// Fetch available libraries
			fmt.Println("Fetching available libraries...")
			manifest, err := mgr.FetchManifest(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to fetch manifest")
				return err
			}

			libs := mgr.GetLibrariesForPlatform(manifest)
			if len(libs) == 0 {
				fmt.Println("No libraries available for this platform")
				return nil
			}

			fmt.Printf("\nAvailable libraries (manifest version: %s):\n", manifest.Version)
			for _, lib := range libs {
				status := ""
				if installedLib, exists := installed.Libraries[lib.Name]; exists {
					if installedLib.Version == lib.Version {
						status = " [installed]"
					} else {
						status = fmt.Sprintf(" [update available: %s -> %s]", installedLib.Version, lib.Version)
					}
				}
				fmt.Printf("  %s (version: %s, size: %d bytes)%s\n", lib.Name, lib.Version, lib.Size, status)
			}

			return nil
		},
	}
	return cmd
}

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download [library...]",
		Short: "Download dependencies to cache",
		Long:  `Download vGPU library dependencies to the local cache without installing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := deps.NewManager(deps.WithCDNBaseURL(cdnURL))
			ctx := context.Background()

			manifest, err := mgr.FetchManifest(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to fetch manifest")
				return err
			}

			libs := mgr.GetLibrariesForPlatform(manifest)
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
				fmt.Printf("Downloading %s (version: %s)...\n", lib.Name, lib.Version)

				progressFn := func(downloaded, total int64) {
					if total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Progress: %.1f%% (%d/%d bytes)", pct, downloaded, total)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					log.Error().Err(err).Str("library", lib.Name).Msg("Failed to download")
					return err
				}
				fmt.Println("\n  Done!")
			}

			fmt.Println("\nAll downloads complete!")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-download even if cached")
	return cmd
}

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [library...]",
		Short: "Download and install dependencies",
		Long:  `Download and install vGPU library dependencies to the system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := deps.NewManager(deps.WithCDNBaseURL(cdnURL))
			ctx := context.Background()

			manifest, err := mgr.FetchManifest(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to fetch manifest")
				return err
			}

			libs := mgr.GetLibrariesForPlatform(manifest)
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
					log.Error().Err(err).Str("library", lib.Name).Msg("Failed to download")
					return err
				}
				fmt.Println()

				// Then install
				fmt.Printf("  Installing to %s...\n", mgr.GetLibraryPath(lib.Name))
				if err := mgr.InstallLibrary(lib); err != nil {
					log.Error().Err(err).Str("library", lib.Name).Msg("Failed to install")
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
			mgr := deps.NewManager(deps.WithCDNBaseURL(cdnURL))
			ctx := context.Background()

			fmt.Println("Checking for updates...")
			updates, err := mgr.CheckUpdates(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to check updates")
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
					log.Error().Err(err).Str("library", lib.Name).Msg("Failed to download")
					continue
				}
				fmt.Println()

				if err := mgr.InstallLibrary(lib); err != nil {
					log.Error().Err(err).Str("library", lib.Name).Msg("Failed to install")
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
			mgr := deps.NewManager(deps.WithCDNBaseURL(cdnURL))

			fmt.Println("Cleaning dependency cache...")
			if err := mgr.CleanCache(); err != nil {
				log.Error().Err(err).Msg("Failed to clean cache")
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
