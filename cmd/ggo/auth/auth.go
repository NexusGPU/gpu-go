package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	tokenFileName   = "token.json"
	dashboardURL    = "https://go.tensor-fusion.ai/settings/security#ide-extension"
	defaultTokenTTL = 365 * 24 * time.Hour // 1 year
)

var outputFormat string

// TokenConfig represents the stored PAT token
type TokenConfig struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// AuthStatusResponse represents the JSON response for auth status
type AuthStatusResponse struct {
	LoggedIn  bool   `json:"logged_in"`
	Token     string `json:"token,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Expired   bool   `json:"expired,omitempty"`
}

// NewAuthCmd creates the auth command
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for GPU Go",
		Long: `Manage authentication tokens for accessing the GPU Go platform.

Use 'ggo login' to authenticate with a Personal Access Token (PAT).
Use 'ggo logout' to remove stored credentials.
Use 'ggo auth status' to check your current authentication status.`,
	}

	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func getOutput() *tui.Output {
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(outputFormat))
}

// NewLoginCmd creates the login command (added to root)
func NewLoginCmd() *cobra.Command {
	var token string
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with GPU Go platform",
		Long: `Authenticate with the GPU Go platform using a Personal Access Token (PAT).

This command will:
1. Open your browser to the GPU Go dashboard
2. Guide you to generate a PAT
3. Store the token securely for future CLI and IDE use

Examples:
  # Interactive login (opens browser)
  ggo login

  # Login with token directly
  ggo login --token <your-pat-token>

  # Login without opening browser
  ggo login --no-browser`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token != "" {
				return saveToken(token)
			}

			return interactiveLogin(noBrowser)
		},
	}

	cmd.Flags().StringVarP(&token, "token", "t", "", "Personal Access Token (PAT)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically")

	return cmd
}

// NewLogoutCmd creates the logout command (added to root)
func NewLogoutCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored authentication credentials",
		Long: `Remove stored Personal Access Token (PAT) from local storage.

This will sign you out of the GPU Go CLI and IDE extensions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenPath := getTokenPath()

			if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
				fmt.Println(tui.InfoMessage("You are not logged in."))
				return nil
			}

			if !force {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Are you sure you want to logout? [y/N]: ",
					styles.Warning.Render("!"))
				reader := bufio.NewReader(os.Stdin)
				confirm, _ := reader.ReadString('\n')
				confirm = strings.TrimSpace(strings.ToLower(confirm))
				if confirm != "y" && confirm != "yes" {
					fmt.Println(tui.InfoMessage("Cancelled."))
					return nil
				}
			}

			if err := os.Remove(tokenPath); err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to remove token")
				return err
			}

			fmt.Println(tui.SuccessMessage("Successfully logged out."))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		Long:  `Display information about your current authentication status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()
			tokenConfig, err := LoadToken()
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to load token")
				return err
			}

			if tokenConfig == nil {
				if out.IsJSON() {
					return out.PrintJSON(AuthStatusResponse{LoggedIn: false})
				}
				fmt.Println(tui.WarningMessage("Not logged in."))
				fmt.Println()
				fmt.Println("Run " + tui.Code("ggo login") + " to authenticate.")
				return nil
			}

			// Check if expired
			expired := !tokenConfig.ExpiresAt.IsZero() && time.Now().After(tokenConfig.ExpiresAt)

			if out.IsJSON() {
				// Mask token for JSON output
				maskedToken := ""
				if len(tokenConfig.Token) > 12 {
					maskedToken = tokenConfig.Token[:8] + "..." + tokenConfig.Token[len(tokenConfig.Token)-4:]
				}
				return out.PrintJSON(AuthStatusResponse{
					LoggedIn:  true,
					Token:     maskedToken,
					CreatedAt: tokenConfig.CreatedAt.Format(time.RFC3339),
					ExpiresAt: tokenConfig.ExpiresAt.Format(time.RFC3339),
					Expired:   expired,
				})
			}

			// Styled output
			styles := tui.DefaultStyles()

			fmt.Println()
			fmt.Println(tui.SuccessMessage("Logged in"))
			fmt.Println()

			// Mask token for display
			maskedToken := tokenConfig.Token[:8] + "..." + tokenConfig.Token[len(tokenConfig.Token)-4:]

			status := tui.NewStatusTable().
				Add("Token", maskedToken).
				Add("Created", tokenConfig.CreatedAt.Format("2006-01-02 15:04:05"))

			if !tokenConfig.ExpiresAt.IsZero() {
				if expired {
					status.AddWithStatus("Expires", tokenConfig.ExpiresAt.Format("2006-01-02 15:04:05")+" (EXPIRED)", "error")
				} else {
					status.Add("Expires", tokenConfig.ExpiresAt.Format("2006-01-02 15:04:05"))
				}
			}

			fmt.Println(status.String())

			if expired {
				fmt.Println()
				fmt.Println(styles.Warning.Render("! Your token has expired. Please run ") + tui.Code("ggo login") + styles.Warning.Render(" to re-authenticate."))
			}

			return nil
		},
	}
}

func interactiveLogin(noBrowser bool) error {
	styles := tui.DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Title.Render("GPU Go Login"))
	fmt.Println()

	if !noBrowser {
		fmt.Println("Opening browser to generate a Personal Access Token (PAT)...")
		fmt.Println()
		fmt.Println("  " + tui.URL(dashboardURL))
		fmt.Println()

		if err := openBrowser(dashboardURL); err != nil {
			log.Warn().Err(err).Msg("Failed to open browser")
			fmt.Println(tui.WarningMessage("Could not open browser automatically."))
		}
	} else {
		fmt.Println("Please visit the following URL to generate a Personal Access Token (PAT):")
		fmt.Println()
		fmt.Println("  " + tui.URL(dashboardURL))
		fmt.Println()
	}

	fmt.Println("After generating your PAT, paste it below.")
	fmt.Println()
	fmt.Print(styles.Bold.Render("Enter PAT: "))

	// Try to read securely (no echo)
	token, err := readSecureInput()
	if err != nil {
		return fmt.Errorf("failed to read token: %w", err)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	return saveToken(token)
}

func readSecureInput() (string, error) {
	// Check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		// Read without echoing
		bytePassword, err := term.ReadPassword(fd)
		fmt.Println() // Print newline after hidden input
		if err != nil {
			return "", err
		}
		return string(bytePassword), nil
	}

	// Not a terminal, read normally (e.g., piped input)
	reader := bufio.NewReader(os.Stdin)
	token, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(token), nil
}

func saveToken(token string) error {
	tokenConfig := &TokenConfig{
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(defaultTokenTTL),
	}

	tokenPath := getTokenPath()

	// Ensure directory exists
	tokenDir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(tokenConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Write atomically
	tmpPath := tokenPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	if err := os.Rename(tmpPath, tokenPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to save token file: %w", err)
	}

	fmt.Println()
	fmt.Println(tui.SuccessMessage("Successfully logged in!"))
	fmt.Println()
	fmt.Println(tui.KeyValue("Token saved to", tokenPath))

	return nil
}

// LoadToken loads the stored PAT token
func LoadToken() (*TokenConfig, error) {
	tokenPath := getTokenPath()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tokenConfig TokenConfig
	if err := json.Unmarshal(data, &tokenConfig); err != nil {
		return nil, err
	}

	return &tokenConfig, nil
}

// GetToken returns the stored PAT token string
func GetToken() (string, error) {
	tokenConfig, err := LoadToken()
	if err != nil {
		return "", err
	}
	if tokenConfig == nil {
		return "", nil
	}
	return tokenConfig.Token, nil
}

// IsLoggedIn returns true if user is logged in
func IsLoggedIn() bool {
	tokenConfig, err := LoadToken()
	if err != nil || tokenConfig == nil {
		return false
	}
	// Check if expired
	if !tokenConfig.ExpiresAt.IsZero() && time.Now().After(tokenConfig.ExpiresAt) {
		return false
	}
	return true
}

func getTokenPath() string {
	paths := platform.DefaultPaths()
	return filepath.Join(paths.UserDir(), tokenFileName)
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // linux and others
		// Try xdg-open first, then common browsers
		candidates := []string{"xdg-open", "x-www-browser", "sensible-browser", "firefox", "chromium", "google-chrome"}
		for _, candidate := range candidates {
			if _, err := exec.LookPath(candidate); err == nil {
				cmd = candidate
				args = []string{url}
				break
			}
		}
		if cmd == "" {
			return fmt.Errorf("no browser found")
		}
	}

	return exec.Command(cmd, args...).Start()
}
