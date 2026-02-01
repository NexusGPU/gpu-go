package deps

import (
	"context"
	"fmt"
	"slices"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
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
	outputFormat    string
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
	cmdutil.AddOutputFlag(cmd, &outputFormat)

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

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync releases metadata from API",
		Long:  `Fetch vendor releases from the API and cache them locally as version metadata.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			targetOS := syncOS
			targetArch := syncArch
			if !out.IsJSON() {
				if targetOS != "" || targetArch != "" {
					fmt.Printf("Syncing releases from API for platform %s/%s...\n", targetOS, targetArch)
				} else {
					fmt.Println("Syncing releases from API...")
				}
			}

			manifest, err := mgr.SyncReleases(ctx, targetOS, targetArch)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&syncResult{manifest: manifest, verbose: verbose})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print verbose synced data")
	cmd.Flags().StringVar(&syncOS, "os", "", "Target OS (linux, darwin, windows). Defaults to current OS")
	cmd.Flags().StringVar(&syncArch, "arch", "", "Target architecture (amd64, arm64). Defaults to current architecture")
	return cmd
}

// syncResult implements Renderable for sync command
type syncResult struct {
	manifest *deps.Manifest
	verbose  bool
}

func (r *syncResult) RenderJSON() any {
	return r.manifest
}

func (r *syncResult) RenderTUI(out *tui.Output) {
	if r.manifest != nil {
		out.Success(fmt.Sprintf("Synced %d libraries (manifest version: %s)", len(r.manifest.Libraries), r.manifest.Version))

		if len(r.manifest.Libraries) > 0 {
			if r.verbose {
				fmt.Println("\nSynced libraries:")
				for _, lib := range r.manifest.Libraries {
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
				for _, lib := range r.manifest.Libraries {
					fmt.Printf("  %s (version: %s, platform: %s/%s)\n", lib.Name, lib.Version, lib.Platform, lib.Arch)
				}
			}
		}
	}
	out.Println("Sync complete!")
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available and installed dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			installed, err := mgr.GetInstalledLibraries()
			if err != nil {
				klog.Warningf("Failed to load installed libraries: error=%v", err)
			}

			downloaded, err := mgr.GetDownloadedLibraries()
			if err != nil {
				klog.Warningf("Failed to load downloaded libraries: error=%v", err)
			}

			manifest, synced, err := mgr.FetchManifest(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			if synced && !out.IsJSON() {
				updates, _ := mgr.CheckUpdates(ctx)
				if len(updates) > 0 {
					out.Info(fmt.Sprintf("Manifest updated. %d updates available. Run 'ggo deps update' to upgrade.", len(updates)))
				}
			}

			var libs []deps.Library
			var filterDesc string
			if listOS == "" && listArch == "" {
				libs = mgr.GetAllLibraries(manifest)
				filterDesc = "all platforms"
			} else {
				libs = mgr.GetLibrariesForPlatform(manifest, listOS, listArch, "")
				if listOS != "" && listArch != "" {
					filterDesc = fmt.Sprintf("%s/%s", listOS, listArch)
				} else if listOS != "" {
					filterDesc = fmt.Sprintf("%s/*", listOS)
				} else {
					filterDesc = fmt.Sprintf("*/%s", listArch)
				}
			}

			slices.SortFunc(libs, func(a, b deps.Library) int {
				if a.Name != b.Name {
					return compareStrings(a.Name, b.Name)
				}
				return compareStrings(a.Platform, b.Platform)
			})

			displayLibs := buildDisplayLibraries(libs, installed, downloaded)

			return out.Render(&listResult{libs: displayLibs, filterDesc: filterDesc})
		},
	}

	cmd.Flags().StringVar(&listOS, "os", "", "Filter by OS (linux, darwin, windows). Omit to list all architectures")
	cmd.Flags().StringVar(&listArch, "arch", "", "Filter by architecture (amd64, arm64). Omit to list all architectures")
	return cmd
}

// DisplayLibrary contains library info with status
type DisplayLibrary struct {
	deps.Library
	Status    string `json:"status"`
	Installed bool   `json:"installed"`
}

func buildDisplayLibraries(libs []deps.Library, installed *deps.LocalManifest, downloaded *deps.DownloadedManifest) []DisplayLibrary {
	displayLibs := make([]DisplayLibrary, len(libs))
	for i, lib := range libs {
		dLib := DisplayLibrary{Library: lib, Status: "Available"}

		if installed != nil {
			if installedLib, exists := installed.Libraries[lib.Name]; exists {
				if installedLib.Version == lib.Version && installedLib.Platform == lib.Platform && installedLib.Arch == lib.Arch {
					dLib.Status = "Installed"
					dLib.Installed = true
					if lib.Size == 0 && installedLib.Size > 0 {
						dLib.Size = installedLib.Size
					}
				} else if installedLib.Version != lib.Version {
					dLib.Status = fmt.Sprintf("Update: %s -> %s", installedLib.Version, lib.Version)
					dLib.Installed = true
				}
			}
		}

		if !dLib.Installed && downloaded != nil {
			if downloadedLib, exists := downloaded.Libraries[lib.Name]; exists {
				if downloadedLib.Version == lib.Version && downloadedLib.Platform == lib.Platform && downloadedLib.Arch == lib.Arch {
					dLib.Status = "Downloaded"
					if lib.Size == 0 && downloadedLib.Size > 0 {
						dLib.Size = downloadedLib.Size
					}
				}
			}
		}
		displayLibs[i] = dLib
	}
	return displayLibs
}

// listResult implements Renderable for list command
type listResult struct {
	libs       []DisplayLibrary
	filterDesc string
}

func (r *listResult) RenderJSON() any {
	return tui.NewListResult(r.libs)
}

func (r *listResult) RenderTUI(out *tui.Output) {
	if len(r.libs) == 0 {
		if r.filterDesc != "all platforms" {
			fmt.Printf("No libraries available for platform %s\n", r.filterDesc)
		} else {
			fmt.Println("No libraries available")
		}
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, dLib := range r.libs {
		style := styles.Muted
		status := dLib.Status

		if status == "Installed" {
			style = styles.Success
		} else if status == "Downloaded" {
			style = styles.Info
		} else if len(status) > 7 && status[:7] == "Update:" {
			style = styles.Warning
		}

		rows = append(rows, []string{
			dLib.Name,
			dLib.Version,
			fmt.Sprintf("%s/%s", dLib.Platform, dLib.Arch),
			formatSize(dLib.Size),
			style.Render(status),
		})
	}

	out.PrintTable([]string{"Name", "Version", "Platform", "Size", "Status"}, rows)
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
			out := getOutput()
			ctx := context.Background()

			manifest, _, err := mgr.FetchManifest(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			targetOS := downloadOS
			targetArch := downloadArch

			libs := mgr.GetLibrariesForPlatform(manifest, targetOS, targetArch, "")
			if len(libs) == 0 {
				platformDesc := "this platform"
				if targetOS != "" || targetArch != "" {
					platformDesc = fmt.Sprintf("%s/%s", targetOS, targetArch)
				}
				return out.Render(&cmdutil.ActionData{
					Success: false,
					Message: fmt.Sprintf("No libraries available for %s", platformDesc),
				})
			}

			filtered := filterLibraries(libs, args, downloadName, downloadVersion)
			if len(filtered) == 0 {
				return out.Render(&cmdutil.ActionData{
					Success: false,
					Message: "No libraries match the specified criteria",
				})
			}

			downloadedLibs := []deps.Library{}
			for _, lib := range filtered {
				if !out.IsJSON() {
					fmt.Printf("Downloading %s (version: %s, platform: %s/%s)...\n", lib.Name, lib.Version, lib.Platform, lib.Arch)
				}

				progressFn := func(downloaded, total int64) {
					if !out.IsJSON() && total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Progress: %.1f%% (%d/%d bytes)", pct, downloaded, total)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					cmd.SilenceUsage = true
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					return err
				}
				if !out.IsJSON() {
					fmt.Println("\n  Done!")
				}
				downloadedLibs = append(downloadedLibs, lib)
			}

			return out.Render(&downloadResult{libs: downloadedLibs})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-download even if cached")
	cmd.Flags().StringVar(&downloadName, "name", "", "Library name to download (e.g., libcuda.so.1)")
	cmd.Flags().StringVar(&downloadVersion, "version", "", "Library version to download")
	cmd.Flags().StringVar(&downloadOS, "os", "", "Target OS (linux, darwin, windows). Defaults to current OS")
	cmd.Flags().StringVar(&downloadArch, "cpuArch", "", "Target CPU architecture (amd64, arm64). Defaults to current architecture")
	return cmd
}

func filterLibraries(libs []deps.Library, args []string, name, version string) []deps.Library {
	var filtered []deps.Library
	for _, lib := range libs {
		if name != "" {
			if lib.Name != name {
				continue
			}
		} else if len(args) > 0 {
			matched := slices.Contains(args, lib.Name)
			if !matched {
				continue
			}
		}

		if version != "" && lib.Version != version {
			continue
		}

		filtered = append(filtered, lib)
	}
	return filtered
}

// downloadResult implements Renderable for download command
type downloadResult struct {
	libs []deps.Library
}

func (r *downloadResult) RenderJSON() any {
	return tui.NewListResult(r.libs)
}

func (r *downloadResult) RenderTUI(out *tui.Output) {
	out.Println("\nAll downloads complete!")
}

func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [library...]",
		Short: "Download and install dependencies",
		Long:  `Download and install vGPU library dependencies to the system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			manifest, _, err := mgr.FetchManifest(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			libs := mgr.GetLibrariesForPlatform(manifest, "", "", "")
			if len(libs) == 0 {
				return out.Render(&cmdutil.ActionData{
					Success: false,
					Message: "No libraries available for this platform",
				})
			}

			if len(args) > 0 {
				var filtered []deps.Library
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

			installedLibs := []deps.Library{}
			for _, lib := range libs {
				if !out.IsJSON() {
					fmt.Printf("Installing %s (version: %s)...\n", lib.Name, lib.Version)
				}

				progressFn := func(downloaded, total int64) {
					if !out.IsJSON() && total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Downloading: %.1f%%", pct)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					cmd.SilenceUsage = true
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					return err
				}
				if !out.IsJSON() {
					fmt.Println()
				}

				if !out.IsJSON() {
					fmt.Printf("  Installing to %s...\n", mgr.GetLibraryPath(lib.Name))
				}
				if err := mgr.InstallLibrary(lib); err != nil {
					cmd.SilenceUsage = true
					klog.Errorf("Failed to install: library=%s error=%v", lib.Name, err)
					return err
				}
				if !out.IsJSON() {
					fmt.Println("  Done!")
				}
				installedLibs = append(installedLibs, lib)
			}

			return out.Render(&installResult{libs: installedLibs, mgr: mgr})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force reinstall even if already installed")
	return cmd
}

// installResult implements Renderable for install command
type installResult struct {
	libs []deps.Library
	mgr  *deps.Manager
}

func (r *installResult) RenderJSON() any {
	return tui.NewListResult(r.libs)
}

func (r *installResult) RenderTUI(out *tui.Output) {
	out.Println("\nAll libraries installed!")
	out.Println("\nTo use the libraries, add the following to your environment:")
	out.Printf("  export LD_PRELOAD=%s\n", r.mgr.GetLibraryPath("libcuda.so.1"))
}

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			if !out.IsJSON() {
				fmt.Println("Checking for updates...")
			}
			updates, err := mgr.CheckUpdates(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to check updates: error=%v", err)
				return err
			}

			if len(updates) == 0 {
				return out.Render(&cmdutil.ActionData{
					Success: true,
					Message: "All libraries are up to date!",
				})
			}

			if !out.IsJSON() {
				fmt.Printf("Found %d updates:\n", len(updates))
				for _, lib := range updates {
					fmt.Printf("  %s: %s\n", lib.Name, lib.Version)
				}
				fmt.Println("\nInstalling updates...")
			}

			installed := []deps.Library{}
			for _, lib := range updates {
				if !out.IsJSON() {
					fmt.Printf("Updating %s...\n", lib.Name)
				}

				progressFn := func(downloaded, total int64) {
					if !out.IsJSON() && total > 0 {
						pct := float64(downloaded) / float64(total) * 100
						fmt.Printf("\r  Progress: %.1f%%", pct)
					}
				}

				if err := mgr.DownloadLibrary(ctx, lib, progressFn); err != nil {
					klog.Errorf("Failed to download: library=%s error=%v", lib.Name, err)
					continue
				}
				if !out.IsJSON() {
					fmt.Println()
				}

				if err := mgr.InstallLibrary(lib); err != nil {
					klog.Errorf("Failed to install: library=%s error=%v", lib.Name, err)
					continue
				}
				if !out.IsJSON() {
					fmt.Println("  Done!")
				}
				installed = append(installed, lib)
			}

			return out.Render(&updateResult{libs: installed})
		},
	}
	return cmd
}

// updateResult implements Renderable for update command
type updateResult struct {
	libs []deps.Library
}

func (r *updateResult) RenderJSON() any {
	return tui.NewListResult(r.libs)
}

func (r *updateResult) RenderTUI(out *tui.Output) {
	out.Println("\nAll updates installed!")
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clean the dependency cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()

			if !out.IsJSON() {
				fmt.Println("Cleaning dependency cache...")
			}
			if err := mgr.CleanCache(); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to clean cache: error=%v", err)
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: "Cache cleaned!",
			})
		},
	}
	return cmd
}
