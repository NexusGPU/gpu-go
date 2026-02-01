package share

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	serverURL    string
	userToken    string
	outputFormat string
)

// NewShareCmd creates the share command
func NewShareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Manage share links for GPU workers",
		Long:  `The share command manages share links that allow others to connect to your GPU workers.`,
	}

	cmd.PersistentFlags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.PersistentFlags().StringVar(&userToken, "token", "", "User authentication token")
	cmdutil.AddOutputFlag(cmd, &outputFormat)

	cmd.AddCommand(newShareCreateCmd())
	cmd.AddCommand(newShareListCmd())
	cmd.AddCommand(newShareDeleteCmd())
	cmd.AddCommand(newShareGetCmd())

	return cmd
}

func getClient() *api.Client {
	token := userToken
	if token == "" {
		token = os.Getenv("GPU_GO_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}
	if token == "" {
		if savedToken, err := auth.GetToken(); err == nil && savedToken != "" {
			token = savedToken
		}
	}
	return api.NewClient(
		api.WithBaseURL(serverURL),
		api.WithUserToken(token),
	)
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

func newShareCreateCmd() *cobra.Command {
	var workerID string
	var connectionIP string
	var expiresIn string
	var maxUses int

	cmd := &cobra.Command{
		Use:   "create <worker-name>",
		Short: "Create a share link for a worker",
		Long:  `Create a shareable link that allows others to connect to your GPU worker.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			if len(args) > 0 && workerID == "" {
				resp, err := client.ListWorkers(ctx, "", "")
				if err != nil {
					cmd.SilenceUsage = true
					klog.Errorf("Failed to list workers: error=%v", err)
					return err
				}

				workerName := args[0]
				for _, w := range resp.Workers {
					if w.Name == workerName {
						workerID = w.WorkerID
						break
					}
				}

				if workerID == "" {
					cmd.SilenceUsage = true
					klog.Errorf("Worker not found: name=%s", workerName)
					return fmt.Errorf("worker '%s' not found", workerName)
				}
			}

			if workerID == "" {
				return fmt.Errorf("worker ID or name is required")
			}

			req := &api.ShareCreateRequest{
				WorkerID:     workerID,
				ConnectionIP: connectionIP,
			}

			if expiresIn != "" {
				duration, err := time.ParseDuration(expiresIn)
				if err != nil {
					return fmt.Errorf("invalid expiration duration: %w", err)
				}
				expiresAt := time.Now().Add(duration)
				req.ExpiresAt = &expiresAt
			}

			if maxUses > 0 {
				req.MaxUses = &maxUses
			}

			resp, err := client.CreateShare(ctx, req)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to create share: error=%v", err)
				return err
			}

			return out.Render(&shareCreateResult{share: resp})
		},
	}

	cmd.Flags().StringVar(&workerID, "worker-id", "", "Worker ID")
	cmd.Flags().StringVar(&connectionIP, "connection-ip", "", "Connection IP address")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "", "Expiration duration (e.g., 24h, 7d)")
	cmd.Flags().IntVar(&maxUses, "max-uses", 0, "Maximum number of uses (0 = unlimited)")

	return cmd
}

// shareCreateResult implements Renderable for share create
type shareCreateResult struct {
	share *api.ShareInfo
}

func (r *shareCreateResult) RenderJSON() any {
	return r.share
}

func (r *shareCreateResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	out.Println()
	out.Success("Share link created successfully!")
	out.Println()

	status := tui.NewStatusTable().
		Add("Short Code", styles.Bold.Render(r.share.ShortCode)).
		Add("Short Link", tui.URL(r.share.ShortLink)).
		Add("Worker ID", r.share.WorkerID).
		Add("Connection URL", r.share.ConnectionURL)

	if r.share.ExpiresAt != nil {
		status.Add("Expires At", r.share.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if r.share.MaxUses != nil {
		status.Add("Max Uses", fmt.Sprintf("%d", *r.share.MaxUses))
	}

	out.Println(status.String())

	out.Println()
	out.Println(styles.Subtitle.Render("Share this with others:"))
	out.Println()
	out.Println("  " + tui.Code(fmt.Sprintf("ggo use %s", r.share.ShortCode)))
	out.Println()
}

func newShareListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all share links",
		Long:  `List all share links for the current user.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			resp, err := client.ListShares(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to list shares: error=%v", err)
				return err
			}

			return out.Render(&shareListResult{shares: resp.Shares})
		},
	}

	return cmd
}

// shareListResult implements Renderable for share list
type shareListResult struct {
	shares []api.ShareInfo
}

func (r *shareListResult) RenderJSON() any {
	return tui.NewListResult(r.shares)
}

func (r *shareListResult) RenderTUI(out *tui.Output) {
	if len(r.shares) == 0 {
		out.Info("No share links found")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, s := range r.shares {
		maxStr := styles.Muted.Render("∞")
		if s.MaxUses != nil {
			maxStr = fmt.Sprintf("%d", *s.MaxUses)
		}
		expiresStr := styles.Muted.Render("never")
		if s.ExpiresAt != nil {
			if s.ExpiresAt.Before(time.Now()) {
				expiresStr = styles.Error.Render("expired")
			} else {
				expiresStr = s.ExpiresAt.Format("2006-01-02")
			}
		}
		rows = append(rows, []string{
			styles.Bold.Render(s.ShortCode),
			tui.URL(s.ShortLink),
			fmt.Sprintf("%d", s.UsedCount),
			maxStr,
			expiresStr,
		})
	}

	table := tui.NewTable().
		Headers("SHORT CODE", "SHORT LINK", "USED", "MAX", "EXPIRES").
		Rows(rows)

	out.Println(table.String())
}

func newShareGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <short-code>",
		Short: "Get share link details",
		Long:  `Get public information about a share link using its short code.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shortCode := args[0]
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			resp, err := client.GetSharePublic(ctx, shortCode)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to get share: error=%v", err)
				return err
			}

			return out.Render(&shareDetailResult{share: resp})
		},
	}

	return cmd
}

// shareDetailResult implements Renderable for share detail
type shareDetailResult struct {
	share *api.SharePublicInfo
}

func (r *shareDetailResult) RenderJSON() any {
	return tui.NewDetailResult(r.share)
}

func (r *shareDetailResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	out.Println()
	out.Println(styles.Title.Render("Share Details"))
	out.Println()

	status := tui.NewStatusTable().
		Add("Worker ID", r.share.WorkerID).
		Add("Hardware Vendor", r.share.HardwareVendor).
		Add("Connection URL", tui.URL(r.share.ConnectionURL))

	out.Println(status.String())
}

func newShareDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <share-id>",
		Short: "Delete a share link",
		Long:  `Delete a share link.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shareID := args[0]
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			if !force && !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Are you sure you want to delete share %s? [y/N]: ",
					styles.Warning.Render("!"),
					styles.Bold.Render(shareID))
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					out.Info("Cancelled")
					return nil
				}
			}

			if err := client.DeleteShare(ctx, shareID); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to delete share: error=%v", err)
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: fmt.Sprintf("Share %s deleted successfully!", shareID),
				ID:      shareID,
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}
