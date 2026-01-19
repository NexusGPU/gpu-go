package share

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
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
	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")

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
	// Fall back to token from ~/.gpugo/token.json
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
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(outputFormat))
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

			// If worker name provided as arg, use it
			if len(args) > 0 && workerID == "" {
				// Look up worker by name
				resp, err := client.ListWorkers(ctx, "", "")
				if err != nil {
					log.Error().Err(err).Msg("Failed to list workers")
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
					log.Error().Str("name", workerName).Msg("Worker not found")
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

			// Parse expiration
			if expiresIn != "" {
				duration, err := time.ParseDuration(expiresIn)
				if err != nil {
					return fmt.Errorf("invalid expiration duration: %w", err)
				}
				expiresAt := time.Now().Add(duration)
				req.ExpiresAt = &expiresAt
			}

			// Set max uses
			if maxUses > 0 {
				req.MaxUses = &maxUses
			}

			resp, err := client.CreateShare(ctx, req)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create share")
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(resp)
			}

			// Styled output
			styles := tui.DefaultStyles()

			fmt.Println()
			fmt.Println(tui.SuccessMessage("Share link created successfully!"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Short Code", styles.Bold.Render(resp.ShortCode)).
				Add("Short Link", tui.URL(resp.ShortLink)).
				Add("Worker ID", resp.WorkerID).
				Add("Connection URL", resp.ConnectionURL)

			if resp.ExpiresAt != nil {
				status.Add("Expires At", resp.ExpiresAt.Format("2006-01-02 15:04:05"))
			}
			if resp.MaxUses != nil {
				status.Add("Max Uses", fmt.Sprintf("%d", *resp.MaxUses))
			}

			fmt.Println(status.String())

			fmt.Println()
			fmt.Println(styles.Subtitle.Render("Share this with others:"))
			fmt.Println()
			fmt.Println("  " + tui.Code(fmt.Sprintf("ggo use %s", resp.ShortCode)))
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVar(&workerID, "worker-id", "", "Worker ID")
	cmd.Flags().StringVar(&connectionIP, "connection-ip", "", "Connection IP address")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "", "Expiration duration (e.g., 24h, 7d)")
	cmd.Flags().IntVar(&maxUses, "max-uses", 0, "Maximum number of uses (0 = unlimited)")

	return cmd
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
				log.Error().Err(err).Msg("Failed to list shares")
				return err
			}

			if len(resp.Shares) == 0 {
				if out.IsJSON() {
					return out.PrintJSON(tui.NewListResult([]api.ShareInfo{}))
				}
				out.Info("No share links found")
				return nil
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewListResult(resp.Shares))
			}

			// Table output
			styles := tui.DefaultStyles()
			var rows [][]string
			for _, s := range resp.Shares {
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

			fmt.Println(table.String())
			return nil
		},
	}

	return cmd
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
				log.Error().Err(err).Msg("Failed to get share")
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewDetailResult(resp))
			}

			// Styled output
			styles := tui.DefaultStyles()

			fmt.Println()
			fmt.Println(styles.Title.Render("Share Details"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Worker ID", resp.WorkerID).
				Add("Hardware Vendor", resp.HardwareVendor).
				Add("Connection URL", tui.URL(resp.ConnectionURL))

			fmt.Println(status.String())
			return nil
		},
	}

	return cmd
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
				log.Error().Err(err).Msg("Failed to delete share")
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewActionResult(true, "Share deleted successfully", shareID))
			}

			fmt.Println()
			fmt.Println(tui.SuccessMessage(fmt.Sprintf("Share %s deleted successfully!", shareID)))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}
