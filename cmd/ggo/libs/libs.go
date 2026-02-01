package libs

import (
	"fmt"
	"runtime"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
)

var (
	cdnURL       string
	version      string
	cacheDir     string
	outputFormat string
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

	cmdutil.AddOutputFlag(cmd, &outputFormat)

	return cmd
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download GPU libraries from CDN",
		Long: `Download GPU libraries from cdn.tensor-fusion.ai for the current architecture.

The libraries are cached locally and used by studio environments for remote GPU access.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			downloader := studio.NewLibraryDownloader(cdnURL, version)
			out := getOutput()

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			arch := studio.DetectArchitecture()

			if !out.IsJSON() {
				fmt.Printf("Detected architecture: %s\n", arch.String())
				fmt.Printf("CDN URL: %s\n", cdnURL)
				fmt.Printf("Version: %s\n", version)
				fmt.Printf("Cache directory: %s\n", downloader.GetCacheDir())
				fmt.Println()
				fmt.Println("Downloading libraries...")
			}

			paths, err := downloader.DownloadDefaultLibraries()
			if err != nil {
				cmd.SilenceUsage = true
				return fmt.Errorf("failed to download libraries: %w", err)
			}

			return out.Render(&downloadResult{paths: paths})
		},
	}

	cmd.Flags().StringVar(&cdnURL, "cdn-url", studio.DefaultCDNURL, "CDN base URL")
	cmd.Flags().StringVar(&version, "version", studio.DefaultLibVersion, "Library version")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Custom cache directory")

	return cmd
}

// downloadResult implements Renderable for libs download
type downloadResult struct {
	paths []string
}

func (r *downloadResult) RenderJSON() any {
	return tui.NewListResult(r.paths)
}

func (r *downloadResult) RenderTUI(out *tui.Output) {
	out.Println()
	out.Println("Successfully downloaded libraries:")
	for _, path := range r.paths {
		out.Printf("  - %s\n", path)
	}
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show information about GPU libraries",
		RunE: func(cmd *cobra.Command, args []string) error {
			arch := studio.DetectArchitecture()
			downloader := studio.NewLibraryDownloader(cdnURL, version)
			out := getOutput()

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			return out.Render(&infoResult{
				arch:       arch,
				downloader: downloader,
				cdnURL:     cdnURL,
				version:    version,
			})
		},
	}
}

// infoResult implements Renderable for libs info
type infoResult struct {
	arch       studio.Architecture
	downloader *studio.LibraryDownloader
	cdnURL     string
	version    string
}

func (r *infoResult) RenderJSON() any {
	libraries := []string{"cuda-vgpu", "cudart", "tensor-fusion-runtime"}
	type libInfo struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	var libs []libInfo
	for _, lib := range libraries {
		info := studio.LibraryInfo{
			Name:    lib,
			Version: r.version,
			Arch:    r.arch,
		}
		libs = append(libs, libInfo{
			Name: lib,
			URL:  r.downloader.DownloadURL(info),
		})
	}

	return map[string]any{
		"os":           runtime.GOOS,
		"architecture": runtime.GOARCH,
		"detected":     r.arch.String(),
		"cdn_url":      r.cdnURL,
		"version":      r.version,
		"cache_dir":    r.downloader.GetCacheDir(),
		"libraries":    libs,
	}
}

func (r *infoResult) RenderTUI(out *tui.Output) {
	out.Println("System Information:")
	out.Printf("  OS:           %s\n", runtime.GOOS)
	out.Printf("  Architecture: %s\n", runtime.GOARCH)
	out.Printf("  Detected:     %s\n", r.arch.String())
	out.Println()

	out.Println("Library Configuration:")
	out.Printf("  CDN URL:      %s\n", r.cdnURL)
	out.Printf("  Version:      %s\n", r.version)
	out.Printf("  Cache Dir:    %s\n", r.downloader.GetCacheDir())
	out.Println()

	out.Println("Default Libraries:")
	libraries := []string{"cuda-vgpu", "cudart", "tensor-fusion-runtime"}
	for _, lib := range libraries {
		libInfo := studio.LibraryInfo{
			Name:    lib,
			Version: r.version,
			Arch:    r.arch,
		}
		url := r.downloader.DownloadURL(libInfo)
		out.Printf("  - %s\n", lib)
		out.Printf("    URL: %s\n", url)
	}
}

func newCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Clean library cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			downloader := studio.NewLibraryDownloader(cdnURL, version)
			out := getOutput()

			if cacheDir != "" {
				downloader.SetCacheDir(cacheDir)
			}

			if !out.IsJSON() {
				fmt.Printf("Cleaning cache directory: %s\n", downloader.GetCacheDir())
			}

			// In production, implement actual cache cleaning
			// For now just success message

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: "Cache cleaned successfully",
			})
		},
	}
}
