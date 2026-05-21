// Package config manages the gchat configuration directory and credential storage.
// Config lives at ~/.config/gchat/ with credentials stored at 0600 permissions.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the gchat config directory path (~/.config/gchat/).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gchat"), nil
}

// CachePath returns the path to the SQLite cache database.
func CachePath() (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.db"), nil
}

// ModelsDir returns the path to the models directory, creating it if needed.
func ModelsDir() (string, error) {
	dir, err := EnsureDir()
	if err != nil {
		return "", err
	}
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0700); err != nil {
		return "", fmt.Errorf("config: cannot create models directory: %w", err)
	}
	return modelsDir, nil
}

// EnsureDir creates the config directory if it doesn't exist.
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("config: cannot create directory: %w", err)
	}

	return dir, nil
}

// Credentials holds stored authentication data.
type Credentials struct {
	Method string `json:"method"` // "cookie" or "oauth"

	// Cookie auth fields
	Cookies      map[string]string `json:"cookies,omitempty"`
	XSRF         string            `json:"xsrf,omitempty"`
	SAPISID      string            `json:"sapisid,omitempty"`
	ChromeProfile string           `json:"chrome_profile,omitempty"`

	// OAuth auth fields
	RefreshToken    string `json:"refresh_token,omitempty"`
	IDToken         string `json:"id_token,omitempty"`
	AccessToken     string `json:"access_token,omitempty"`
	OAuthClientID   string `json:"oauth_client_id,omitempty"`
	OAuthClientSec  string `json:"oauth_client_secret,omitempty"`

	// Shared
	SelfGaiaID string `json:"self_gaia_id,omitempty"`
}

// credentialsPath returns the full path to the credentials file.
func credentialsPath(dir string) string {
	return filepath.Join(dir, "credentials.json")
}

// SaveCredentials writes credentials to disk with restrictive permissions.
func SaveCredentials(creds *Credentials) error {
	dir, err := EnsureDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("config: cannot marshal credentials: %w", err)
	}

	path := credentialsPath(dir)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config: cannot write credentials: %w", err)
	}

	return nil
}

// LoadCredentials reads stored credentials from disk.
// Returns nil and no error if the file doesn't exist.
func LoadCredentials() (*Credentials, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	path := credentialsPath(dir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: cannot read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("config: cannot parse credentials: %w", err)
	}

	return &creds, nil
}

// DeleteCredentials removes the credentials file.
func DeleteCredentials() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	path := credentialsPath(dir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("config: cannot delete credentials: %w", err)
	}

	return nil
}
