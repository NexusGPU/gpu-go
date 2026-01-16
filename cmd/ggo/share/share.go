package share

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	serverURL string
	userToken string
)

// NewShareCmd creates the share command
func NewShareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Manage share links for GPU workers",
		Long:  `The share command manages share links that allow others to connect to your GPU workers.`,
	}

	cmd.PersistentFlags().StringVar(&serverURL, "server", "https://api.gpu.tf", "Server URL")
	cmd.PersistentFlags().StringVar(&userToken, "token", "", "User authentication token")

	cmd.AddCommand(newShareCreateCmd())
	cmd.AddCommand(newShareListCmd())
	cmd.AddCommand(newShareDeleteCmd())
	cmd.AddCommand(newShareGetCmd())

	return cmd
}

func getClient() *api.Client {
	token := userToken
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}
	return api.NewClient(
		api.WithBaseURL(serverURL),
		api.WithUserToken(token),
	)
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

			fmt.Println("\n🔗 Share link created successfully!")
			fmt.Println()
			fmt.Printf("  Short Link:     %s\n", resp.ShortLink)
			fmt.Printf("  Short Code:     %s\n", resp.ShortCode)
			fmt.Printf("  Worker ID:      %s\n", resp.WorkerID)
			fmt.Printf("  Connection URL: %s\n", resp.ConnectionURL)

			if resp.ExpiresAt != nil {
				fmt.Printf("  Expires At:     %s\n", resp.ExpiresAt.Format("2006-01-02 15:04:05"))
			}
			if resp.MaxUses != nil {
				fmt.Printf("  Max Uses:       %d\n", *resp.MaxUses)
			}
			fmt.Println()
			fmt.Println("Share this link with others to give them access to your GPU worker:")
			fmt.Printf("\n  ggo use %s\n\n", resp.ShortCode)

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

			resp, err := client.ListShares(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to list shares")
				return err
			}

			if len(resp.Shares) == 0 {
				log.Info().Msg("No share links found")
				return nil
			}

			fmt.Printf("%-15s %-25s %-10s %-10s %-20s\n", "SHORT CODE", "SHORT LINK", "USED", "MAX", "EXPIRES")
			fmt.Println("--------------------------------------------------------------------------------")
			for _, s := range resp.Shares {
				maxStr := "∞"
				if s.MaxUses != nil {
					maxStr = fmt.Sprintf("%d", *s.MaxUses)
				}
				expiresStr := "never"
				if s.ExpiresAt != nil {
					expiresStr = s.ExpiresAt.Format("2006-01-02")
				}
				fmt.Printf("%-15s %-25s %-10d %-10s %-20s\n", s.ShortCode, s.ShortLink, s.UsedCount, maxStr, expiresStr)
			}

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

			resp, err := client.GetSharePublic(ctx, shortCode)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get share")
				return err
			}

			fmt.Printf("Worker ID:       %s\n", resp.WorkerID)
			fmt.Printf("Hardware Vendor: %s\n", resp.HardwareVendor)
			fmt.Printf("Connection URL:  %s\n", resp.ConnectionURL)

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

			if !force {
				fmt.Printf("Are you sure you want to delete share %s? [y/N]: ", shareID)
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					log.Info().Msg("Cancelled")
					return nil
				}
			}

			if err := client.DeleteShare(ctx, shareID); err != nil {
				log.Error().Err(err).Msg("Failed to delete share")
				return err
			}

			log.Info().Str("share_id", shareID).Msg("Share deleted successfully")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}
