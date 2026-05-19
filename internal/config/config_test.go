// Package config tests credential storage and config directory management.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveAndLoadCredentials verifies the roundtrip of credential persistence.
func TestSaveAndLoadCredentials(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	creds := &Credentials{
		Method: "cookie",
		Cookies: map[string]string{
			"SID":  "test_sid",
			"HSID": "test_hsid",
		},
		XSRF: "test_xsrf",
	}

	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}

	loaded, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadCredentials returned nil")
	}

	if loaded.Method != "cookie" {
		t.Errorf("Method: got %q, want 'cookie'", loaded.Method)
	}
	if loaded.Cookies["SID"] != "test_sid" {
		t.Errorf("SID cookie: got %q, want 'test_sid'", loaded.Cookies["SID"])
	}
	if loaded.XSRF != "test_xsrf" {
		t.Errorf("XSRF: got %q, want 'test_xsrf'", loaded.XSRF)
	}
}

// TestLoadCredentialsNotExist verifies nil return when no credentials file exists.
func TestLoadCredentialsNotExist(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil credentials, got %+v", creds)
	}
}

// TestCredentialsFilePermissions verifies the credentials file has 0600 permissions.
func TestCredentialsFilePermissions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	creds := &Credentials{Method: "oauth", RefreshToken: "secret"}
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}

	path := filepath.Join(tmp, ".config", "gchat", "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions: got %o, want 0600", perm)
	}
}

// TestDeleteCredentials verifies credential removal.
func TestDeleteCredentials(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	creds := &Credentials{Method: "cookie"}
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}

	if err := DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials failed: %v", err)
	}

	loaded, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials after delete failed: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil after delete, got %+v", loaded)
	}
}
