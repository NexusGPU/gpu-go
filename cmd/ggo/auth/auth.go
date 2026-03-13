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

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"k8s.io/klog/v2"
)

const (
	tokenFileName   = "token.json"
	dashboardURL    = "https://tensor-fusion.ai/settings/security#ide-extension"
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

	cmdutil.AddOutputFlag(cmd, &outputFormat)
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
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
			out := getOutput()
			if token != "" {
				return saveToken(token, out)
			}
			return interactiveLogin(noBrowser, out)
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
			out := getOutput()
			tokenPath := getTokenPath()

			if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
				return out.Render(&cmdutil.ActionData{
					Success: false,
					Message: "You are not logged in",
				})
			}

			if !force && !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Are you sure you want to logout? [y/N]: ",
					styles.Warning.Render("!"))
				reader := bufio.NewReader(os.Stdin)
				confirm, _ := reader.ReadString('\n')
				confirm = strings.TrimSpace(strings.ToLower(confirm))
				if confirm != "y" && confirm != "yes" {
					out.Info("Cancelled.")
					return nil
				}
			}

			if err := os.Remove(tokenPath); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to remove token: error=%v", err)
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: "Successfully logged out",
			})
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
				cmd.SilenceUsage = true
				klog.Errorf("Failed to load token: error=%v", err)
				return err
			}

			if tokenConfig == nil {
				return out.Render(&authStatusResult{tokenConfig: nil})
			}

			return out.Render(&authStatusResult{tokenConfig: tokenConfig})
		},
	}
}

// authStatusResult implements Renderable for auth status
type authStatusResult struct {
	tokenConfig *TokenConfig
}

func (r *authStatusResult) RenderJSON() any {
	if r.tokenConfig == nil {
		return AuthStatusResponse{LoggedIn: false}
	}

	expired := !r.tokenConfig.ExpiresAt.IsZero() && time.Now().After(r.tokenConfig.ExpiresAt)
	maskedToken := ""
	if len(r.tokenConfig.Token) > 12 {
		maskedToken = r.tokenConfig.Token[:8] + "..." + r.tokenConfig.Token[len(r.tokenConfig.Token)-4:]
	}

	return AuthStatusResponse{
		LoggedIn:  true,
		Token:     maskedToken,
		CreatedAt: r.tokenConfig.CreatedAt.Format(time.RFC3339),
		ExpiresAt: r.tokenConfig.ExpiresAt.Format(time.RFC3339),
		Expired:   expired,
	}
}

func (r *authStatusResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	if r.tokenConfig == nil {
		out.Warning("Not logged in.")
		out.Println()
		out.Println("Run " + tui.Code("ggo login") + " to authenticate.")
		return
	}

	expired := !r.tokenConfig.ExpiresAt.IsZero() && time.Now().After(r.tokenConfig.ExpiresAt)
	maskedToken := r.tokenConfig.Token[:8] + "..." + r.tokenConfig.Token[len(r.tokenConfig.Token)-4:]

	out.Println()
	out.Success("Logged in")
	out.Println()

	status := tui.NewStatusTable().
		Add("Token", maskedToken).
		Add("Created", r.tokenConfig.CreatedAt.Format("2006-01-02 15:04:05"))

	if !r.tokenConfig.ExpiresAt.IsZero() {
		if expired {
			status.AddWithStatus("Expires", r.tokenConfig.ExpiresAt.Format("2006-01-02 15:04:05")+" (EXPIRED)", "error")
		} else {
			status.Add("Expires", r.tokenConfig.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
	}

	out.Println(status.String())

	if expired {
		out.Println()
		out.Println(styles.Warning.Render("! Your token has expired. Please run ") + tui.Code("ggo login") + styles.Warning.Render(" to re-authenticate."))
	}
}

func interactiveLogin(noBrowser bool, out *tui.Output) error {
	styles := tui.DefaultStyles()

	if !out.IsJSON() {
		fmt.Println()
		fmt.Println(styles.Title.Render("GPU Go Login"))
		fmt.Println()

		if !noBrowser {
			fmt.Println("Opening browser to generate a Personal Access Token (PAT)...")
			fmt.Println()
			fmt.Println("  " + tui.URL(dashboardURL))
			fmt.Println()

			if err := openBrowser(dashboardURL); err != nil {
				klog.Warningf("Failed to open browser: error=%v", err)
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
	}

	token, err := readSecureInput()
	if err != nil {
		return fmt.Errorf("failed to read token: %w", err)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	return saveToken(token, out)
}

func readSecureInput() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		bytePassword, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return string(bytePassword), nil
	}

	reader := bufio.NewReader(os.Stdin)
	token, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(token), nil
}

func saveToken(token string, out *tui.Output) error {
	tokenConfig := &TokenConfig{
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(defaultTokenTTL),
	}

	tokenPath := getTokenPath()

	tokenDir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(tokenConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	tmpPath := tokenPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	if err := os.Rename(tmpPath, tokenPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to save token file: %w", err)
	}

	return out.Render(&loginResult{tokenPath: tokenPath})
}

// loginResult implements Renderable for login result
type loginResult struct {
	tokenPath string
}

func (r *loginResult) RenderJSON() any {
	return tui.NewActionResult(true, "Successfully logged in", "")
}

func (r *loginResult) RenderTUI(out *tui.Output) {
	out.Println()
	out.Success("Successfully logged in!")
	out.Println()
	out.Println(tui.KeyValue("Token saved to", r.tokenPath))
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
	default:
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
