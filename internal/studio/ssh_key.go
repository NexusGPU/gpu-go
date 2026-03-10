package studio

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"k8s.io/klog/v2"
)

// GetOrCreateStudioSSHKey gets or creates a dedicated SSH key pair for TF studio containers
// Returns the public key content, private key path, and error if any
func GetOrCreateStudioSSHKey() (publicKey string, privateKeyPath string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Store dedicated SSH keys in ~/.ggo/ssh/
	sshDir := filepath.Join(homeDir, ".ggo", "ssh")
	privateKeyPath = filepath.Join(sshDir, "id_ed25519")
	publicKeyPath := filepath.Join(sshDir, "id_ed25519.pub")

	// Check if key pair already exists
	if pubContent, err := os.ReadFile(publicKeyPath); err == nil {
		publicKey = strings.TrimSpace(string(pubContent))
		if publicKey != "" {
			klog.V(2).Infof("Using existing TF studio SSH key: %s", publicKeyPath)
			return publicKey, privateKeyPath, nil
		}
	}

	// Generate new Ed25519 key pair
	klog.Infof("Generating new SSH key pair for TF studio containers...")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create SSH directory: %w", err)
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SSH key: %w", err)
	}

	// Convert to SSH format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH public key: %w", err)
	}

	// Write public key
	publicKeyData := ssh.MarshalAuthorizedKey(sshPubKey)
	if err := os.WriteFile(publicKeyPath, publicKeyData, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write public key: %w", err)
	}

	// Write private key in PEM format
	privKeyBytes, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	privKeyPEM := pem.EncodeToMemory(privKeyBytes)
	if err := os.WriteFile(privateKeyPath, privKeyPEM, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}

	publicKey = strings.TrimSpace(string(publicKeyData))
	klog.Infof("Generated new SSH key pair: %s", publicKeyPath)
	klog.Infof("Private key saved to: %s", privateKeyPath)

	return publicKey, privateKeyPath, nil
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
