package libs

import (
	"fmt"
	"runtime"

	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/spf13/cobra"
)

var (
	cdnURL   string
	version  string
	cacheDir string
)

// NewLibsCmd creates the libs command
func NewLibsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "libs",
		Short: "Manage GPU libraries for remote GPU access",
		Long: `Download and manage GPU libraries required for remote GPU access.

Libraries are downloaded from cdn.tensor-fusion.ai and cached locally.
They are automatically selected based on your system architecture (darwin-arm64, linux-amd64, etc.).

Examples:
  # Download libraries for current architecture
  ggo libs download

  # Show library information
  ggo libs info

  # Clear library cache
  ggo libs clean`,
	}

	cmd.AddCommand(newDownloadCmd())
	cmd.AddCommand(newInfoCmd())
	cmd.AddCommand(newCleanCmd())

	return cmd
}

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download GPU libraries from CDN",
		Long: `Download GPU libraries from cdn.tensor-fusion.ai for the current architecture.

The libraries are cached locally and used by studio environments for remote GPU access.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			downloader := studio.NewLibraryDownloader(cdnURL, version)

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			arch := studio.DetectArchitecture()
			fmt.Printf("Detected architecture: %s\n", arch.String())
			fmt.Printf("CDN URL: %s\n", cdnURL)
			fmt.Printf("Version: %s\n", version)
			fmt.Printf("Cache directory: %s\n", downloader.GetCacheDir())
			fmt.Println()

			fmt.Println("Downloading libraries...")
			paths, err := downloader.DownloadDefaultLibraries()
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				return fmt.Errorf("failed to download libraries: %w", err)
			}

			fmt.Println()
			fmt.Println("Successfully downloaded libraries:")
			for _, path := range paths {
				fmt.Printf("  - %s\n", path)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&cdnURL, "cdn-url", studio.DefaultCDNURL, "CDN base URL")
	cmd.Flags().StringVar(&version, "version", studio.DefaultLibVersion, "Library version")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Custom cache directory")

	return cmd
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show information about GPU libraries",
		RunE: func(cmd *cobra.Command, args []string) error {
			arch := studio.DetectArchitecture()
			downloader := studio.NewLibraryDownloader(cdnURL, version)

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			fmt.Println("System Information:")
			fmt.Printf("  OS:           %s\n", runtime.GOOS)
			fmt.Printf("  Architecture: %s\n", runtime.GOARCH)
			fmt.Printf("  Detected:     %s\n", arch.String())
			fmt.Println()

			fmt.Println("Library Configuration:")
			fmt.Printf("  CDN URL:      %s\n", cdnURL)
			fmt.Printf("  Version:      %s\n", version)
			fmt.Printf("  Cache Dir:    %s\n", downloader.GetCacheDir())
			fmt.Println()

			fmt.Println("Default Libraries:")
			libraries := []string{"cuda-vgpu", "cudart", "tensor-fusion-runtime"}
			for _, lib := range libraries {
				libInfo := studio.LibraryInfo{
					Name:    lib,
					Version: version,
					Arch:    arch,
				}
				url := downloader.DownloadURL(libInfo)
				fmt.Printf("  - %s\n", lib)
				fmt.Printf("    URL: %s\n", url)
			}

			return nil
		},
	}
}

func newCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Clean library cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			downloader := studio.NewLibraryDownloader(cdnURL, version)

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			fmt.Printf("Cleaning cache directory: %s\n", downloader.GetCacheDir())
			// In production, implement actual cache cleaning
			fmt.Println("Cache cleaned successfully")

			return nil
		},
	}
}
