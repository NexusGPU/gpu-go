package studio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

// GetUserSSHPublicKey attempts to read user's SSH public key from standard locations
// Returns the public key content or empty string if not found
func GetUserSSHPublicKey() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		klog.V(2).Infof("Failed to get user home directory: %v", err)
		return ""
	}

	// Try common SSH public key files in order of preference
	keyPaths := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519.pub"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa.pub"),
		filepath.Join(homeDir, ".ssh", "id_rsa.pub"),
		filepath.Join(homeDir, ".ssh", "id_dsa.pub"),
	}

	for _, keyPath := range keyPaths {
		if content, err := os.ReadFile(keyPath); err == nil {
			publicKey := strings.TrimSpace(string(content))
			if publicKey != "" {
				klog.V(2).Infof("Found SSH public key: %s", keyPath)
				return publicKey
			}
		}
	}

	klog.V(2).Info("No SSH public key found in standard locations")
	return ""
}

// FormatSSHPublicKey formats and validates an SSH public key
// Returns the formatted key or an error if invalid
func FormatSSHPublicKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty SSH public key")
	}

	// Basic validation: should start with ssh-rsa, ssh-ed25519, ecdsa-sha2, etc.
	validPrefixes := []string{"ssh-rsa", "ssh-ed25519", "ecdsa-sha2-", "ssh-dss"}
	valid := false
	for _, prefix := range validPrefixes {
		if strings.HasPrefix(key, prefix) {
			valid = true
			break
		}
	}

	if !valid {
		return "", fmt.Errorf("invalid SSH public key format (must start with ssh-rsa, ssh-ed25519, etc.)")
	}

	return key, nil
}
