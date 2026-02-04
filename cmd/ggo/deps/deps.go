package deps

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

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
	autoConfirm     bool // -y flag for auto-confirmation
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
		Long:  `Fetch vendor releases from the API and cache them locally as release manifest.`,
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
	manifest *deps.ReleaseManifest
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
					fmt.Printf("    Type: %s\n", lib.Type)
					fmt.Printf("    Platform: %s/%s\n", lib.Platform, lib.Arch)
					fmt.Printf("    Size: %d bytes\n", lib.Size)
					fmt.Printf("    SHA256: %s\n", lib.SHA256)
					fmt.Printf("    URL: %s\n", lib.URL)
					fmt.Println()
				}
			} else {
				fmt.Println("\nSynced libraries:")
				for _, lib := range r.manifest.Libraries {
					typeStr := ""
					if lib.Type != "" {
						typeStr = fmt.Sprintf(" [%s]", lib.Type)
					}
					fmt.Printf("  %s (version: %s, platform: %s/%s)%s\n", lib.Name, lib.Version, lib.Platform, lib.Arch, typeStr)
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

			depsManifest, err := mgr.LoadDepsManifest()
			if err != nil {
				klog.Warningf("Failed to load deps manifest: error=%v", err)
			}

			downloaded, err := mgr.LoadDownloadedManifest()
			if err != nil {
				klog.Warningf("Failed to load downloaded manifest: error=%v", err)
			}

			manifest, synced, err := mgr.FetchReleaseManifest(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to fetch manifest: error=%v", err)
				return err
			}

			if synced && !out.IsJSON() {
				diff, _ := mgr.ComputeUpdateDiff()
				if diff != nil && len(diff.ToDownload) > 0 {
					out.Info(fmt.Sprintf("Manifest updated. %d updates available. Run 'ggo deps update' to upgrade.", len(diff.ToDownload)))
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

			// Build merged display libraries (group by name+platform+arch, show latest version)
			displayLibs := buildMergedDisplayLibraries(libs, depsManifest, downloaded)

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
	Status         string   `json:"status"`
	Installed      bool     `json:"installed"`
	Downloaded     bool     `json:"downloaded"`
	AvailVersions  []string `json:"available_versions,omitempty"`
	InstalledVer   string   `json:"installed_version,omitempty"`
	DownloadedVer  string   `json:"downloaded_version,omitempty"`
	FileExists     bool     `json:"file_exists"`
	ActualFileSize int64    `json:"actual_file_size,omitempty"`
}

// buildMergedDisplayLibraries groups libraries by key (name+platform+arch) and merges versions
func buildMergedDisplayLibraries(libs []deps.Library, depsManifest *deps.DepsManifest, downloaded *deps.DownloadedManifest) []DisplayLibrary {
	// Group by key
	grouped := make(map[string][]deps.Library)
	for _, lib := range libs {
		key := lib.Key()
		grouped[key] = append(grouped[key], lib)
	}

	// Build display list
	var displayLibs []DisplayLibrary
	for _, libGroup := range grouped {
		if len(libGroup) == 0 {
			continue
		}

		// Sort by version descending to get latest first
		sort.Slice(libGroup, func(i, j int) bool {
			return libGroup[i].Version > libGroup[j].Version
		})

		// Use latest version as the primary display
		latestLib := libGroup[0]
		dLib := DisplayLibrary{
			Library: latestLib,
			Status:  "Available",
		}

		// Collect all available versions
		for _, lib := range libGroup {
			dLib.AvailVersions = append(dLib.AvailVersions, lib.Version)
		}

		key := latestLib.Key()

		// Check deps manifest (what's required/installed)
		if depsManifest != nil {
			if installedLib, exists := depsManifest.Libraries[key]; exists {
				dLib.InstalledVer = installedLib.Version
				dLib.Installed = true

				// Use installed lib's size if current is 0
				if dLib.Size == 0 && installedLib.Size > 0 {
					dLib.Size = installedLib.Size
				}
			}
		}

		// Check downloaded manifest
		if downloaded != nil {
			if downloadedLib, exists := downloaded.Libraries[key]; exists {
				dLib.DownloadedVer = downloadedLib.Version
				dLib.Downloaded = true

				// Use downloaded lib's size if current is 0
				if dLib.Size == 0 && downloadedLib.Size > 0 {
					dLib.Size = downloadedLib.Size
				}
			}
		}

		// Check if file actually exists and get actual size
		mgr := getManager()
		filePath := mgr.GetLibraryPath(latestLib.Name)
		if info, err := os.Stat(filePath); err == nil {
			dLib.FileExists = true
			dLib.ActualFileSize = info.Size()
			// Always use actual file size if available
			if dLib.ActualFileSize > 0 {
				dLib.Size = dLib.ActualFileSize
			}
		}

		// Determine status
		dLib.Status = determineStatus(dLib)

		displayLibs = append(displayLibs, dLib)
	}

	// Sort by name
	slices.SortFunc(displayLibs, func(a, b DisplayLibrary) int {
		if a.Name != b.Name {
			return compareStrings(a.Name, b.Name)
		}
		return compareStrings(a.Platform, b.Platform)
	})

	return displayLibs
}

func determineStatus(dLib DisplayLibrary) string {
	// If file doesn't exist, cannot be installed/downloaded
	if !dLib.FileExists {
		if dLib.Installed && dLib.InstalledVer != "" {
			return fmt.Sprintf("Missing (v%s)", dLib.InstalledVer)
		}
		return "Available"
	}

	// File exists - check versions
	if dLib.Installed {
		if dLib.InstalledVer == dLib.Version {
			return "Installed"
		}
		return fmt.Sprintf("Update: %s → %s", dLib.InstalledVer, dLib.Version)
	}

	if dLib.Downloaded {
		if dLib.DownloadedVer == dLib.Version {
			return "Downloaded"
		}
		return fmt.Sprintf("Downloaded (%s)", dLib.DownloadedVer)
	}

	return "Available"
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

		switch {
		case strings.HasPrefix(status, "Installed"):
			style = styles.Success
		case strings.HasPrefix(status, "Downloaded"):
			style = styles.Info
		case strings.HasPrefix(status, "Update:"):
			style = styles.Warning
		case strings.HasPrefix(status, "Missing"):
			style = styles.Error
		}

		typeStr := dLib.Type
		if typeStr == "" {
			typeStr = "-"
		}

		// Show "N/A" for size when file doesn't exist and size is 0
		sizeStr := formatSize(dLib.Size)
		if !dLib.FileExists && dLib.Size == 0 {
			sizeStr = "N/A"
		}

		rows = append(rows, []string{
			dLib.Name,
			dLib.Version,
			typeStr,
			fmt.Sprintf("%s/%s", dLib.Platform, dLib.Arch),
			sizeStr,
			style.Render(status),
		})
	}

	out.PrintTable([]string{"Name", "Version", "Type", "Platform", "Size", "Status"}, rows)
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
		Long: `Download vGPU library dependencies to the local cache.
Downloads all libraries in deps-manifest that aren't already downloaded or have version mismatches.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			// Ensure deps manifest exists
			depsManifest, err := mgr.LoadDepsManifest()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			if depsManifest == nil || len(depsManifest.Libraries) == 0 {
				if !out.IsJSON() {
					out.Info("No dependencies configured. Running 'ggo deps update' first...")
				}
				_, _, err := mgr.UpdateDepsManifest(ctx)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}
			}

			// Download all required libraries
			if !out.IsJSON() {
				fmt.Println("Downloading dependencies...")
			}

			progressFn := func(lib deps.Library, downloaded, total int64) {
				if !out.IsJSON() && total > 0 {
					pct := float64(downloaded) / float64(total) * 100
					fmt.Printf("\r  %s: %.1f%% (%d/%d bytes)", lib.Name, pct, downloaded, total)
				}
			}

			results, err := mgr.DownloadAllRequired(ctx, progressFn)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&downloadResult{results: results})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-download even if cached")
	cmd.Flags().StringVar(&downloadName, "name", "", "Library name to download (e.g., libcuda.so.1)")
	cmd.Flags().StringVar(&downloadVersion, "version", "", "Library version to download")
	cmd.Flags().StringVar(&downloadOS, "os", "", "Target OS (linux, darwin, windows). Defaults to current OS")
	cmd.Flags().StringVar(&downloadArch, "cpuArch", "", "Target CPU architecture (amd64, arm64). Defaults to current architecture")
	return cmd
}

// downloadResult implements Renderable for download command
type downloadResult struct {
	results []deps.DownloadResult
}

func (r *downloadResult) RenderJSON() any {
	return tui.NewListResult(r.results)
}

func (r *downloadResult) RenderTUI(out *tui.Output) {
	if len(r.results) == 0 {
		out.Info("No dependencies to download")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string

	var newCount, updatedCount, existingCount, failedCount int

	for _, res := range r.results {
		style := styles.Muted
		statusStr := string(res.Status)

		switch res.Status {
		case deps.DownloadStatusNew:
			style = styles.Success
			statusStr = "✓ New"
			newCount++
		case deps.DownloadStatusUpdated:
			style = styles.Warning
			statusStr = "✓ Updated"
			updatedCount++
		case deps.DownloadStatusExisting:
			style = styles.Info
			statusStr = "• Existing"
			existingCount++
		case deps.DownloadStatusFailed:
			style = styles.Error
			statusStr = "✗ Failed"
			failedCount++
		}

		typeStr := res.Library.Type
		if typeStr == "" {
			typeStr = "-"
		}

		rows = append(rows, []string{
			res.Library.Name,
			res.Library.Version,
			typeStr,
			formatSize(res.Library.Size),
			style.Render(statusStr),
		})
	}

	fmt.Println()
	out.PrintTable([]string{"Name", "Version", "Type", "Size", "Status"}, rows)
	fmt.Println()

	// Summary
	var summary []string
	if newCount > 0 {
		summary = append(summary, fmt.Sprintf("%d new", newCount))
	}
	if updatedCount > 0 {
		summary = append(summary, fmt.Sprintf("%d updated", updatedCount))
	}
	if existingCount > 0 {
		summary = append(summary, fmt.Sprintf("%d existing", existingCount))
	}
	if failedCount > 0 {
		summary = append(summary, fmt.Sprintf("%d failed", failedCount))
	}

	if len(summary) > 0 {
		out.Println(fmt.Sprintf("Download complete: %s", strings.Join(summary, ", ")))
	}
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

			manifest, _, err := mgr.FetchReleaseManifest(ctx)
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
}

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install updates",
		Long: `Sync releases from API, update deps manifest, and optionally download updates.
Use -y flag to automatically download without confirmation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			out := getOutput()
			ctx := context.Background()

			if !out.IsJSON() {
				fmt.Println("Syncing releases and checking for updates...")
			}

			// Update deps manifest (syncs releases internally)
			newDeps, changes, err := mgr.UpdateDepsManifest(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to update deps manifest: error=%v", err)
				return err
			}

			// Compute diff between deps manifest and downloaded
			diff, err := mgr.ComputeUpdateDiff()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			if len(diff.ToDownload) == 0 {
				return out.Render(&cmdutil.ActionData{
					Success: true,
					Message: "All dependencies are up to date!",
				})
			}

			// Show what needs to be downloaded
			if !out.IsJSON() {
				fmt.Printf("\nFound %d dependencies to update:\n\n", len(diff.ToDownload))

				styles := tui.DefaultStyles()
				var rows [][]string
				for _, lib := range diff.ToDownload {
					typeStr := lib.Type
					if typeStr == "" {
						typeStr = "-"
					}
					rows = append(rows, []string{
						lib.Name,
						lib.Version,
						typeStr,
						fmt.Sprintf("%s/%s", lib.Platform, lib.Arch),
						formatSize(lib.Size),
						styles.Warning.Render("Pending"),
					})
				}
				out.PrintTable([]string{"Name", "Version", "Type", "Platform", "Size", "Status"}, rows)
				fmt.Println()
			}

			// If not auto-confirm, prompt user
			if !autoConfirm && !out.IsJSON() {
				fmt.Print("Do you want to download these updates? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					out.Info("Update cancelled")
					return nil
				}
			}

			// Download updates
			if !out.IsJSON() {
				fmt.Println("\nDownloading updates...")
			}

			progressFn := func(lib deps.Library, downloaded, total int64) {
				if !out.IsJSON() && total > 0 {
					pct := float64(downloaded) / float64(total) * 100
					fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
				}
			}

			results, err := mgr.DownloadAllRequired(ctx, progressFn)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&updateResult{
				deps:    newDeps,
				changes: changes,
				results: results,
			})
		},
	}

	cmd.Flags().BoolVarP(&autoConfirm, "yes", "y", false, "Automatically confirm and download updates")
	return cmd
}

// updateResult implements Renderable for update command
type updateResult struct {
	deps    *deps.DepsManifest
	changes []deps.Library
	results []deps.DownloadResult
}

func (r *updateResult) RenderJSON() any {
	return map[string]any{
		"deps_manifest": r.deps,
		"changes":       r.changes,
		"downloads":     r.results,
	}
}

func (r *updateResult) RenderTUI(out *tui.Output) {
	if len(r.results) == 0 {
		out.Success("All dependencies are up to date!")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string

	var successCount, failedCount int
	for _, res := range r.results {
		style := styles.Success
		statusStr := "✓ Downloaded"

		switch res.Status {
		case deps.DownloadStatusFailed:
			style = styles.Error
			statusStr = "✗ Failed"
			failedCount++
		case deps.DownloadStatusExisting:
			style = styles.Info
			statusStr = "• Up to date"
		default:
			successCount++
		}

		rows = append(rows, []string{
			res.Library.Name,
			res.Library.Version,
			style.Render(statusStr),
		})
	}

	fmt.Println()
	out.PrintTable([]string{"Name", "Version", "Status"}, rows)
	fmt.Println()

	if failedCount > 0 {
		out.Warning(fmt.Sprintf("%d updates failed", failedCount))
	} else {
		out.Success(fmt.Sprintf("All %d updates installed!", successCount))
	}
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
